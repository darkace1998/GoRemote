package sftp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live SFTP browser session. The user interacts with a
// classic shell-style REPL (cwd-prompt, line-buffered commands) over
// the host's terminal pane.
//
// Safe for concurrent Start / SendInput / Close / Resize per the
// [protocol.Session] contract.
type Session struct {
	sc      *pkgsftp.Client
	sshC    *ssh.Client
	cleanup func()

	remoteCWD string
	localCWD  string

	// inBuf accumulates partial lines arriving via SendInput when the
	// caller doesn't go through the Start stdin path (e.g. paste).
	inMu  sync.Mutex
	inBuf bytes.Buffer

	// outMu serialises writes to the host stdout writer. The terminal
	// widget can be written to from multiple goroutines (resize echo,
	// user echo, command output), so a single mutex prevents
	// interleaving.
	outMu sync.Mutex
	out   io.Writer

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}
}

func newSession(sc *pkgsftp.Client, sshC *ssh.Client, cleanup func(), initialPath string) *Session {
	cwd := initialPath
	if cwd == "" {
		if h, err := sc.Getwd(); err == nil && h != "" {
			cwd = h
		} else {
			cwd = "/"
		}
	}
	local, _ := os.Getwd()
	if local == "" {
		local = "."
	}
	return &Session{
		sc:        sc,
		sshC:      sshC,
		cleanup:   cleanup,
		remoteCWD: cwd,
		localCWD:  local,
		closed:    make(chan struct{}),
	}
}

// RenderMode reports the rendering mode. SFTP renders through the host
// terminal as an interactive shell.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the REPL until ctx is cancelled, the client closes the
// stdin pipe, or the user types `exit`/`quit`. Returns nil on clean
// shutdowns.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		// Without a terminal sink, the REPL has nothing to render to.
		// Block on ctx so the caller can still drive the session via
		// SendInput, but no command output is produced.
		<-ctx.Done()
		return s.Close()
	}

	s.outMu.Lock()
	s.out = stdout
	s.outMu.Unlock()

	s.banner()
	s.writePrompt()

	// React to ctx cancellation by closing the underlying SSH client,
	// which unblocks any pending SFTP RPC.
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.closed:
		}
	}()

	if stdin != nil {
		// Drive the REPL from the stdin pipe. Reads a line at a time
		// so we can keep the prompt model consistent regardless of
		// whether the host buffers locally.
		r := bufio.NewReader(stdin)
		for {
			line, err := readLine(r)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
					return s.Close()
				}
				if ctx.Err() != nil {
					return s.Close()
				}
				_ = s.writeLine("[stdin error: " + err.Error() + "]")
				return s.Close()
			}
			if cont := s.handleLine(ctx, line); !cont {
				return s.Close()
			}
			s.writePrompt()
		}
	}

	// stdin == nil: drain inBuf in a small ticker loop; SendInput
	// callers feed bytes into inBuf, we slice off complete lines.
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return s.Close()
		case <-s.closed:
			return nil
		case <-tick.C:
			for {
				line, ok := s.popLine()
				if !ok {
					break
				}
				if cont := s.handleLine(ctx, line); !cont {
					return s.Close()
				}
				s.writePrompt()
			}
		}
	}
}

// banner writes the welcome lines shown right after Open succeeds.
func (s *Session) banner() {
	_ = s.writeLine("goremote SFTP — connected. Type 'help' for commands, 'exit' to disconnect.")
}

// writePrompt writes the cwd-prompt expected before each user input.
func (s *Session) writePrompt() {
	_ = s.writeRaw([]byte("sftp:" + s.remoteCWD + "> "))
}

// writeLine writes s + CRLF in a single locked Write so output from
// concurrent callers (resize, command output) never interleaves.
func (s *Session) writeLine(line string) error {
	return s.writeRaw([]byte(line + "\r\n"))
}

func (s *Session) writeRaw(b []byte) error {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	if s.out == nil {
		return nil
	}
	_, err := s.out.Write(b)
	return err
}

// handleLine dispatches a single user-entered command. Returns false if
// the session should terminate.
func (s *Session) handleLine(ctx context.Context, line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	args := tokenize(line)
	if len(args) == 0 {
		return true
	}
	cmd, args := strings.ToLower(args[0]), args[1:]
	switch cmd {
	case "exit", "quit", "bye":
		_ = s.writeLine("Goodbye.")
		return false
	case "help", "?":
		s.helpCmd()
	case "pwd":
		_ = s.writeLine(s.remoteCWD)
	case "lpwd":
		_ = s.writeLine(s.localCWD)
	case "cd":
		s.cdCmd(args)
	case "lcd":
		s.lcdCmd(args)
	case "ls", "dir":
		s.lsCmd(args)
	case "lls":
		s.llsCmd(args)
	case "mkdir":
		s.mkdirCmd(args)
	case "rmdir":
		s.rmdirCmd(args)
	case "rm":
		s.rmCmd(args)
	case "mv", "rename":
		s.mvCmd(args)
	case "chmod":
		s.chmodCmd(args)
	case "get":
		s.getCmd(ctx, args)
	case "put":
		s.putCmd(ctx, args)
	default:
		_ = s.writeLine("unknown command: " + cmd + " (type 'help')")
	}
	return true
}

func (s *Session) helpCmd() {
	lines := []string{
		"Commands:",
		"  pwd                 print remote working directory",
		"  lpwd                print local working directory",
		"  cd <path>           change remote directory",
		"  lcd <path>          change local directory",
		"  ls [path]           list remote directory",
		"  lls [path]          list local directory",
		"  mkdir <path>        create remote directory",
		"  rmdir <path>        remove remote directory",
		"  rm <path>           remove remote file",
		"  mv <src> <dst>      rename / move remote file",
		"  chmod <mode> <path> change permissions (octal, e.g. 0644)",
		"  get <remote> [local]  download file",
		"  put <local> [remote]  upload file",
		"  help                this message",
		"  exit                disconnect",
	}
	for _, l := range lines {
		_ = s.writeLine(l)
	}
}

func (s *Session) cdCmd(args []string) {
	if len(args) == 0 {
		// `cd` with no args goes home.
		if h, err := s.sc.Getwd(); err == nil && h != "" {
			s.remoteCWD = h
		}
		return
	}
	target := s.resolveRemote(args[0])
	st, err := s.sc.Stat(target)
	if err != nil {
		_ = s.writeLine("cd: " + err.Error())
		return
	}
	if !st.IsDir() {
		_ = s.writeLine("cd: not a directory: " + target)
		return
	}
	s.remoteCWD = path.Clean(target)
}

func (s *Session) lcdCmd(args []string) {
	if len(args) == 0 {
		if h, err := os.UserHomeDir(); err == nil {
			s.localCWD = h
		}
		return
	}
	target := args[0]
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.localCWD, target)
	}
	st, err := os.Stat(target)
	if err != nil {
		_ = s.writeLine("lcd: " + err.Error())
		return
	}
	if !st.IsDir() {
		_ = s.writeLine("lcd: not a directory: " + target)
		return
	}
	s.localCWD = filepath.Clean(target)
}

func (s *Session) lsCmd(args []string) {
	target := s.remoteCWD
	if len(args) > 0 {
		target = s.resolveRemote(args[0])
	}
	entries, err := s.sc.ReadDir(target)
	if err != nil {
		_ = s.writeLine("ls: " + err.Error())
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		mode := e.Mode().String()
		size := e.Size()
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		_ = s.writeLine(fmt.Sprintf("%s %10d %s", mode, size, name))
	}
}

func (s *Session) llsCmd(args []string) {
	target := s.localCWD
	if len(args) > 0 {
		target = args[0]
		if !filepath.IsAbs(target) {
			target = filepath.Join(s.localCWD, target)
		}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		_ = s.writeLine("lls: " + err.Error())
		return
	}
	for _, e := range entries {
		info, _ := e.Info()
		var sz int64
		var mode string
		if info != nil {
			sz = info.Size()
			mode = info.Mode().String()
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		_ = s.writeLine(fmt.Sprintf("%s %10d %s", mode, sz, name))
	}
}

func (s *Session) mkdirCmd(args []string) {
	if len(args) == 0 {
		_ = s.writeLine("mkdir: missing path")
		return
	}
	target := s.resolveRemote(args[0])
	if err := s.sc.Mkdir(target); err != nil {
		_ = s.writeLine("mkdir: " + err.Error())
	}
}

func (s *Session) rmdirCmd(args []string) {
	if len(args) == 0 {
		_ = s.writeLine("rmdir: missing path")
		return
	}
	target := s.resolveRemote(args[0])
	if err := s.sc.RemoveDirectory(target); err != nil {
		_ = s.writeLine("rmdir: " + err.Error())
	}
}

func (s *Session) rmCmd(args []string) {
	if len(args) == 0 {
		_ = s.writeLine("rm: missing path")
		return
	}
	target := s.resolveRemote(args[0])
	if err := s.sc.Remove(target); err != nil {
		_ = s.writeLine("rm: " + err.Error())
	}
}

func (s *Session) mvCmd(args []string) {
	if len(args) < 2 {
		_ = s.writeLine("mv: usage: mv <src> <dst>")
		return
	}
	src := s.resolveRemote(args[0])
	dst := s.resolveRemote(args[1])
	if err := s.sc.Rename(src, dst); err != nil {
		_ = s.writeLine("mv: " + err.Error())
	}
}

func (s *Session) chmodCmd(args []string) {
	if len(args) < 2 {
		_ = s.writeLine("chmod: usage: chmod <octal-mode> <path>")
		return
	}
	var mode os.FileMode
	if _, err := fmt.Sscanf(args[0], "%o", &mode); err != nil {
		_ = s.writeLine("chmod: bad mode: " + args[0])
		return
	}
	target := s.resolveRemote(args[1])
	if err := s.sc.Chmod(target, mode); err != nil {
		_ = s.writeLine("chmod: " + err.Error())
	}
}

func (s *Session) getCmd(ctx context.Context, args []string) {
	if len(args) == 0 {
		_ = s.writeLine("get: missing remote path")
		return
	}
	remote := s.resolveRemote(args[0])
	local := filepath.Base(remote)
	if len(args) > 1 {
		local = args[1]
	}
	if !filepath.IsAbs(local) {
		local = filepath.Join(s.localCWD, local)
	}

	rf, err := s.sc.Open(remote)
	if err != nil {
		_ = s.writeLine("get: open remote: " + err.Error())
		return
	}
	defer func() { _ = rf.Close() }()

	lf, err := os.Create(local)
	if err != nil {
		_ = s.writeLine("get: create local: " + err.Error())
		return
	}
	defer func() { _ = lf.Close() }()

	n, err := s.copyWithCancel(ctx, lf, rf)
	if err != nil {
		_ = s.writeLine(fmt.Sprintf("get: %s (%d bytes copied)", err.Error(), n))
		return
	}
	_ = s.writeLine(fmt.Sprintf("get: %s -> %s (%d bytes)", remote, local, n))
}

func (s *Session) putCmd(ctx context.Context, args []string) {
	if len(args) == 0 {
		_ = s.writeLine("put: missing local path")
		return
	}
	local := args[0]
	if !filepath.IsAbs(local) {
		local = filepath.Join(s.localCWD, local)
	}
	remote := path.Base(local)
	if len(args) > 1 {
		remote = args[1]
	}
	remote = s.resolveRemote(remote)

	lf, err := os.Open(local)
	if err != nil {
		_ = s.writeLine("put: open local: " + err.Error())
		return
	}
	defer func() { _ = lf.Close() }()

	rf, err := s.sc.Create(remote)
	if err != nil {
		_ = s.writeLine("put: create remote: " + err.Error())
		return
	}
	defer func() { _ = rf.Close() }()

	n, err := s.copyWithCancel(ctx, rf, lf)
	if err != nil {
		_ = s.writeLine(fmt.Sprintf("put: %s (%d bytes copied)", err.Error(), n))
		return
	}
	_ = s.writeLine(fmt.Sprintf("put: %s -> %s (%d bytes)", local, remote, n))
}

// copyWithCancel is io.Copy with a periodic ctx-cancellation check so
// large transfers honor session shutdown promptly.
func (s *Session) copyWithCancel(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	const buf = 64 * 1024
	b := make([]byte, buf)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, rerr := src.Read(b)
		if n > 0 {
			if _, werr := dst.Write(b[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return total, nil
			}
			return total, rerr
		}
	}
}

// resolveRemote returns an absolute remote path, joining relative paths
// against the current remote CWD using POSIX-style separators (SFTP
// servers always use forward slashes).
func (s *Session) resolveRemote(p string) string {
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Clean(path.Join(s.remoteCWD, p))
}

// Resize is a no-op — the SFTP REPL doesn't depend on a terminal grid
// size. Returning nil keeps host resize events silent.
func (s *Session) Resize(_ context.Context, _ protocol.Size) error { return nil }

// SendInput appends data to the input buffer; the Start loop slices
// complete lines off in its tick loop.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	select {
	case <-s.closed:
		return errors.New("sftp: send-input on closed session")
	default:
	}
	s.inMu.Lock()
	s.inBuf.Write(data)
	s.inMu.Unlock()
	return nil
}

// popLine removes and returns the next complete line from inBuf.
// Returns ok=false when no \n is present yet.
func (s *Session) popLine() (string, bool) {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	idx := bytes.IndexByte(s.inBuf.Bytes(), '\n')
	if idx < 0 {
		return "", false
	}
	line := s.inBuf.Next(idx + 1)
	return strings.TrimRight(string(line), "\r\n"), true
}

// Close terminates the session. Idempotent under concurrent callers.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.sc != nil {
			_ = s.sc.Close()
		}
		if s.sshC != nil {
			s.closeErr = s.sshC.Close()
		}
		if s.cleanup != nil {
			s.cleanup()
		}
	})
	return s.closeErr
}

// readLine reads one line, stripping the trailing CR/LF. Backed by a
// bufio.Reader so partial reads (TTY echo, paste) coalesce correctly.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), err
}

// tokenize splits a command line on whitespace, honoring single and
// double quotes so paths containing spaces work as one argument.
func tokenize(line string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble, escape := false, false, false
	for _, r := range line {
		switch {
		case escape:
			cur.WriteRune(r)
			escape = false
		case r == '\\' && !inSingle:
			escape = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
