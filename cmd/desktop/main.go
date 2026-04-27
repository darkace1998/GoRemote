// Package main is the goremote desktop entry point.
//
// This program wires the built-in protocol and credential plugins into an
// application core and exposes a stable Bindings struct whose methods are
// called from the Fyne GUI or, in headless mode, via a small interactive
// JSON-RPC loop on stdin/stdout useful for smoke-testing.
//
// The Fyne-based GUI is always compiled; CGO is required.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/goremote/goremote/app/settings"
	"github.com/goremote/goremote/app/workspace"
	pluginhost "github.com/goremote/goremote/host/plugin"
	"github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/domain"
	"github.com/goremote/goremote/internal/logging"
	"github.com/goremote/goremote/internal/platform"
	sdkcred "github.com/goremote/goremote/sdk/credential"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
	protocol "github.com/goremote/goremote/sdk/protocol"

	credfile "github.com/goremote/goremote/plugins/credential-file"
	credkeychain "github.com/goremote/goremote/plugins/credential-keychain"
	protohttp "github.com/goremote/goremote/plugins/protocol-http"
	protomosh "github.com/goremote/goremote/plugins/protocol-mosh"
	protopowershell "github.com/goremote/goremote/plugins/protocol-powershell"
	protoraw "github.com/goremote/goremote/plugins/protocol-rawsocket"
	protordp "github.com/goremote/goremote/plugins/protocol-rdp"
	protorlogin "github.com/goremote/goremote/plugins/protocol-rlogin"
	protossh "github.com/goremote/goremote/plugins/protocol-ssh"
	prototelnet "github.com/goremote/goremote/plugins/protocol-telnet"
	prototn5250 "github.com/goremote/goremote/plugins/protocol-tn5250"
	protovnc "github.com/goremote/goremote/plugins/protocol-vnc"
)

// Bindings is the stable surface exposed to the UI. Every method accepts and
// returns only JSON-serialisable values so that any bridge can marshal them
// without bespoke converters.
//
// Methods are grouped:
//   - Tree commands: ListTree, CreateFolder, CreateConnection, UpdateConnection,
//     UpdateFolder, MoveNode, DeleteNode, GetConnection, Search.
//   - Sessions: OpenSession, SendInput, Resize, CloseSession, ListSessions.
//   - Credentials: UnlockCredentialProvider, LockCredentialProvider,
//     CredentialProviderState.
//   - Import/export: ImportMRemoteNG, ExportSnapshot, RestoreSnapshot.
//
// New methods added here MUST be backward compatible or bump the documented
// bridge version in webui/src/bridge/bridge.ts.
type Bindings struct {
	app       *app.App
	logger    *slog.Logger
	settings  settings.Store
	workspace workspace.Store
}

// NewBindings constructs a Bindings value wrapping a started App.
func NewBindings(a *app.App) *Bindings {
	return &Bindings{
		app:       a,
		logger:    a.Logger().With(slog.String("component", "bindings")),
		settings:  nil,
		workspace: nil,
	}
}

// WithSettingsStore attaches a settings store to the bindings. Returns the
// receiver so it can be chained at construction time.
func (b *Bindings) WithSettingsStore(s settings.Store) *Bindings {
	b.settings = s
	return b
}

// WithWorkspaceStore attaches a workspace store to the bindings.
func (b *Bindings) WithWorkspaceStore(s workspace.Store) *Bindings {
	b.workspace = s
	return b
}

// --- Tree commands ------------------------------------------------------

// ListTree returns the full tree as a UI-friendly view.
func (b *Bindings) ListTree(ctx context.Context) app.TreeView { return b.app.ListTree(ctx) }

// CreateFolder creates a folder under parent (empty parent == root).
func (b *Bindings) CreateFolder(ctx context.Context, parent string, name string, description string, tags []string) (string, error) {
	pid, err := parseID(parent)
	if err != nil {
		return "", err
	}
	id, err := b.app.CreateFolder(ctx, pid, name, app.FolderOpts{Description: description, Tags: tags})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// CreateConnection creates a connection under parent.
func (b *Bindings) CreateConnection(ctx context.Context, parent string, opts ConnectionInput) (string, error) {
	pid, err := parseID(parent)
	if err != nil {
		return "", err
	}
	id, err := b.app.CreateConnection(ctx, pid, opts.toAppOpts())
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// UpdateConnection applies a patch to an existing connection.
func (b *Bindings) UpdateConnection(ctx context.Context, id string, patch ConnectionPatchInput) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	return b.app.UpdateConnection(ctx, nid, patch.toAppPatch())
}

// UpdateFolder applies a patch to an existing folder.
func (b *Bindings) UpdateFolder(ctx context.Context, id string, patch FolderPatchInput) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	return b.app.UpdateFolder(ctx, nid, patch.toAppPatch())
}

// MoveNode reparents a node.
func (b *Bindings) MoveNode(ctx context.Context, id, newParent string) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	pid, err := parseID(newParent)
	if err != nil {
		return err
	}
	return b.app.MoveNode(ctx, nid, pid)
}

// DeleteNode removes a folder or connection.
func (b *Bindings) DeleteNode(ctx context.Context, id string) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	return b.app.DeleteNode(ctx, nid)
}

// GetConnection returns a resolved view of a single connection.
func (b *Bindings) GetConnection(ctx context.Context, id string) (app.ConnectionView, error) {
	nid, err := parseID(id)
	if err != nil {
		return app.ConnectionView{}, err
	}
	return b.app.GetConnection(ctx, nid)
}

// Search queries the tree.
func (b *Bindings) Search(ctx context.Context, q app.SearchQuery) []app.NodeView {
	return b.app.Search(ctx, q)
}

// --- Session commands ---------------------------------------------------

// OpenSession starts a session for a connection and returns the handle.
func (b *Bindings) OpenSession(ctx context.Context, connectionID string) (string, error) {
	nid, err := parseID(connectionID)
	if err != nil {
		return "", err
	}
	h, err := b.app.OpenSession(ctx, nid)
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// SendInput writes bytes to a session.
func (b *Bindings) SendInput(ctx context.Context, handle string, data []byte) error {
	h, err := parseID(handle)
	if err != nil {
		return err
	}
	return b.app.SendInput(ctx, h, data)
}

// Resize updates the PTY dimensions for a session.
func (b *Bindings) Resize(ctx context.Context, handle string, cols, rows uint16) error {
	h, err := parseID(handle)
	if err != nil {
		return err
	}
	return b.app.Resize(ctx, h, cols, rows)
}

// CloseSession terminates a session.
func (b *Bindings) CloseSession(ctx context.Context, handle string) error {
	h, err := parseID(handle)
	if err != nil {
		return err
	}
	return b.app.CloseSession(ctx, h)
}

// ListSessions returns currently open sessions.
func (b *Bindings) ListSessions() []app.SessionInfo { return b.app.ListSessions() }

// --- Import/export ------------------------------------------------------

// ImportMRemoteNGFile imports from a file on disk.
func (b *Bindings) ImportMRemoteNGFile(ctx context.Context, format, path string) (app.ImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return app.ImportResult{}, err
	}
	defer f.Close()
	return b.app.ImportMRemoteNG(ctx, format, f)
}

// ExportSnapshot creates a zip backup of the current state.
func (b *Bindings) ExportSnapshot(ctx context.Context) (app.BackupInfo, error) {
	return b.app.ExportSnapshot(ctx)
}

// RestoreSnapshot restores from a zip backup.
func (b *Bindings) RestoreSnapshot(ctx context.Context, path string) error {
	return b.app.RestoreSnapshot(ctx, path)
}

// --- Settings -----------------------------------------------------------

// GetSettings returns the persisted user settings, or defaults if no
// settings have been written yet (or if the file is missing/corrupt).
func (b *Bindings) GetSettings(ctx context.Context) (settings.Settings, error) {
	if b.settings == nil {
		return settings.Default(), nil
	}
	return b.settings.Get(ctx)
}

// UpdateSettings validates the supplied settings and persists them.
func (b *Bindings) UpdateSettings(ctx context.Context, s settings.Settings) (settings.Settings, error) {
	if b.settings == nil {
		return settings.Settings{}, errors.New("settings store not initialised")
	}
	return b.settings.Update(ctx, s)
}

// --- Workspace ----------------------------------------------------------

// GetWorkspace returns the persisted workspace document, or defaults
// when no document has been written yet (or the file is missing/corrupt).
// The returned document is always Validate-clean.
func (b *Bindings) GetWorkspace(ctx context.Context) (workspace.Workspace, error) {
	if b.workspace == nil {
		return workspace.Default(), nil
	}
	w, err := b.workspace.Load(ctx)
	if err != nil {
		return workspace.Default(), err
	}
	// Defensive: Load already validates, but if a future bug ever lets
	// invalid state through, fall back to defaults rather than handing
	// the UI broken state.
	if verr := w.Validate(); verr != nil {
		b.logger.Error("workspace: load returned invalid state", slog.String("err", verr.Error()))
		return workspace.Default(), nil
	}
	return w, nil
}

// SaveWorkspace validates and persists the supplied workspace.
func (b *Bindings) SaveWorkspace(ctx context.Context, w workspace.Workspace) error {
	if b.workspace == nil {
		return errors.New("workspace store not initialised")
	}
	return b.workspace.Save(ctx, w)
}

// --- Input helpers ------------------------------------------------------

// ConnectionInput mirrors app.ConnectionOpts but with string IDs for
// CredentialRef so the UI does not need to know the domain.ID type.
type ConnectionInput struct {
	Name          string             `json:"name"`
	ProtocolID    string             `json:"protocolId"`
	Host          string             `json:"host"`
	Port          int                `json:"port"`
	Username      string             `json:"username"`
	CredentialRef CredentialRefInput `json:"credentialRef"`
	Description   string             `json:"description"`
	Tags          []string           `json:"tags"`
	Settings      map[string]any     `json:"settings"`
	Environment   string             `json:"environment"`
}

// CredentialRefInput is the JSON form of credential.Reference.
type CredentialRefInput struct {
	ProviderID string `json:"providerId"`
	Key        string `json:"key"`
}

func (in ConnectionInput) toAppOpts() app.ConnectionOpts {
	return app.ConnectionOpts{
		Name:          in.Name,
		ProtocolID:    in.ProtocolID,
		Host:          in.Host,
		Port:          in.Port,
		Username:      in.Username,
		CredentialRef: credRefFromInput(in.CredentialRef),
		Description:   in.Description,
		Tags:          in.Tags,
		Settings:      in.Settings,
		Environment:   in.Environment,
	}
}

// ConnectionPatchInput is the JSON form of app.ConnectionPatch.
type ConnectionPatchInput struct {
	Name          *string             `json:"name,omitempty"`
	ProtocolID    *string             `json:"protocolId,omitempty"`
	Host          *string             `json:"host,omitempty"`
	Port          *int                `json:"port,omitempty"`
	Username      *string             `json:"username,omitempty"`
	CredentialRef *CredentialRefInput `json:"credentialRef,omitempty"`
	Description   *string             `json:"description,omitempty"`
	Tags          *[]string           `json:"tags,omitempty"`
	Settings      *map[string]any     `json:"settings,omitempty"`
	Environment   *string             `json:"environment,omitempty"`
}

func (p ConnectionPatchInput) toAppPatch() app.ConnectionPatch {
	out := app.ConnectionPatch{
		Name:        p.Name,
		ProtocolID:  p.ProtocolID,
		Host:        p.Host,
		Port:        p.Port,
		Username:    p.Username,
		Description: p.Description,
		Tags:        p.Tags,
		Settings:    p.Settings,
		Environment: p.Environment,
	}
	if p.CredentialRef != nil {
		ref := credRefFromInput(*p.CredentialRef)
		out.CredentialRef = &ref
	}
	return out
}

// FolderPatchInput mirrors app.FolderPatch for JSON transit.
type FolderPatchInput struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

func (p FolderPatchInput) toAppPatch() app.FolderPatch {
	return app.FolderPatch{
		Name:        p.Name,
		Description: p.Description,
		Tags:        p.Tags,
	}
}

// --- main ---------------------------------------------------------------

func main() {
	logLevel := resolveLogLevel(os.Getenv("GOREMOTE_LOG_LEVEL"))
	logger := logging.New(logging.Options{
		Writer: chooseLogWriter(runtime.GOOS, os.Stderr, os.Stdout),
		Level:  logLevel,
	})

	dir, err := resolveStateDir()
	if err != nil {
		logger.Error("resolve state dir", slog.String("err", err.Error()))
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Error("create state dir", slog.String("err", err.Error()))
		os.Exit(1)
	}

	a, err := newAppWithRecovery(app.Config{Dir: dir, Logger: logger}, logger)
	if err != nil {
		logger.Error("app.New", slog.String("err", err.Error()))
		os.Exit(1)
	}

	bootCtx, bootCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bootCancel()
	if err := a.Start(bootCtx); err != nil {
		logger.Error("app.Start", slog.String("err", err.Error()))
		os.Exit(1)
	}

	if err := registerBuiltins(bootCtx, a, dir); err != nil {
		logger.Error("register builtins", slog.String("err", err.Error()))
		_ = a.Shutdown(context.Background())
		os.Exit(1)
	}

	bindings := NewBindings(a)

	// Settings store. Keep failures non-fatal: the UI falls back to defaults.
	if sp, err := settings.DefaultPath(); err != nil {
		logger.Error("settings: resolve path", slog.String("err", err.Error()))
	} else {
		bindings.WithSettingsStore(settings.NewFileStoreWithLogger(sp, settings.NewSlogLogger(logger)))
	}

	// Workspace store. Failures are non-fatal: the UI starts with an
	// empty workspace.
	if wp, err := workspace.DefaultPath(); err != nil {
		logger.Error("workspace: resolve path", slog.String("err", err.Error()))
	} else {
		bindings.WithWorkspaceStore(workspace.NewFileStore(wp, workspace.NewSlogLogger(logger)))
	}

	// Hand off to the Fyne GUI. If runGUI returns false for any reason,
	// fall through to a tiny interactive JSON-RPC loop on stdin/stdout
	// that is sufficient for smoke tests and scripted automation.
	if runGUI(a, bindings) {
		// The Fyne GUI owns the main loop. On return, shut down cleanly.
		shutdown(a, logger)
		return
	}

	// Headless / CLI mode.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runCLI(ctx, bindings, logger)
	shutdown(a, logger)
}

func chooseLogWriter(goos string, stderr io.Writer, stdout io.Writer) io.Writer {
	if goos == "windows" {
		return stdout
	}
	return stderr
}

func resolveLogLevel(v string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "trace":
		return slog.Level(-8)
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// shutdown tears down the application with a bounded timeout.
func shutdown(a *app.App, logger *slog.Logger) {
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	if err := a.Shutdown(sctx); err != nil {
		logger.Error("app.Shutdown", slog.String("err", err.Error()))
	}
}

// newAppWithRecovery constructs the app and recovers once from corrupted
// on-disk state by quarantining the state directory and retrying with a
// fresh store.
func newAppWithRecovery(cfg app.Config, logger *slog.Logger) (*app.App, error) {
	a, err := app.New(cfg)
	if err == nil {
		return a, nil
	}
	if !strings.Contains(err.Error(), "app: load snapshot:") {
		return nil, err
	}
	quarantineDir, qerr := quarantineStateDir(cfg.Dir)
	if qerr != nil {
		return nil, fmt.Errorf("%w (quarantine failed: %v)", err, qerr)
	}
	logger.Error("state load failed; quarantined state and retrying",
		slog.String("dir", cfg.Dir),
		slog.String("quarantineDir", quarantineDir),
		slog.String("err", err.Error()))
	return app.New(cfg)
}

func quarantineStateDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("empty state dir")
	}
	dst := filepath.Join(
		filepath.Dir(dir),
		fmt.Sprintf("%s.corrupt-%d", filepath.Base(dir), time.Now().UnixNano()),
	)
	if err := os.Rename(dir, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// registerBuiltins registers the in-process protocol and credential plugins.
func registerBuiltins(ctx context.Context, a *app.App, dir string) error {
	logger := a.Logger().With(slog.String("component", "bootstrap"))
	protos := []struct {
		name string
		mod  protocol.Module
	}{
		{"ssh", protossh.New()},
		{"telnet", prototelnet.New()},
		{"rlogin", protorlogin.New()},
		{"rawsocket", protoraw.New()},
		{"http", protohttp.New()},
		{"powershell", protopowershell.New()},
		{"rdp", protordp.New()},
		{"vnc", protovnc.New()},
		{"tn5250", prototn5250.New()},
		{"mosh", protomosh.New()},
	}
	for _, p := range protos {
		if err := a.RegisterProtocol(ctx, p.mod, sdkplugin.TrustCore); err != nil {
			if errors.Is(err, pluginhost.ErrPlatformUnsupported) {
				logger.Warn("protocol unsupported on this platform; skipping",
					slog.String("protocol", p.name),
					slog.String("err", err.Error()))
				continue
			}
			return fmt.Errorf("register %s: %w", p.name, err)
		}
	}

	vaultPath := filepath.Join(dir, "credentials.vault")
	if err := a.RegisterCredential(ctx, credfile.New(vaultPath), sdkplugin.TrustCore); err != nil {
		if errors.Is(err, pluginhost.ErrPlatformUnsupported) {
			logger.Warn("credential provider unsupported on this platform; skipping",
				slog.String("provider", "credential-file"),
				slog.String("err", err.Error()))
		} else {
			return fmt.Errorf("register credential-file: %w", err)
		}
	}

	kc := platform.NewKeychain()
	paths := platform.NewPaths()
	if err := a.RegisterCredential(ctx, credkeychain.New(kc, paths), sdkplugin.TrustCore); err != nil {
		if errors.Is(err, pluginhost.ErrPlatformUnsupported) {
			logger.Warn("credential provider unsupported on this platform; skipping",
				slog.String("provider", "credential-keychain"),
				slog.String("err", err.Error()))
			return nil
		}
		return fmt.Errorf("register credential-keychain: %w", err)
	}
	return nil
}

// resolveStateDir picks the on-disk state directory per platform.Paths.
func resolveStateDir() (string, error) {
	if v := os.Getenv("GOREMOTE_STATE_DIR"); v != "" {
		return v, nil
	}
	p := platform.NewPaths()
	d, err := p.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state"), nil
}

// parseID accepts the empty string (root) and a stringified domain.ID.
func parseID(s string) (domain.ID, error) {
	if s == "" {
		return domain.ID{}, nil
	}
	return domain.ParseID(s)
}

func credRefFromInput(in CredentialRefInput) sdkcred.Reference {
	return sdkcred.Reference{ProviderID: in.ProviderID, EntryID: in.Key}
}

// rpcCall is one call in the CLI JSON-RPC loop.
type rpcCall struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcReply struct {
	ID     int    `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// runCLI runs a small newline-delimited JSON-RPC loop so that the desktop
// binary is exercisable without the Fyne GUI. Methods supported are a
// minimal subset intended for smoke tests.
func runCLI(ctx context.Context, b *Bindings, logger *slog.Logger) {
	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(os.Stdin)
	logger.Info("goremote headless: newline-delimited JSON-RPC on stdin/stdout; send {\"id\":1,\"method\":\"ListTree\"} to begin")

	for {
		if ctx.Err() != nil {
			return
		}
		var call rpcCall
		if err := dec.Decode(&call); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = enc.Encode(rpcReply{Error: "decode: " + err.Error()})
			continue
		}
		reply := dispatch(ctx, b, call)
		if err := enc.Encode(reply); err != nil {
			logger.Error("encode reply", slog.String("err", err.Error()))
			return
		}
	}
}

func dispatch(ctx context.Context, b *Bindings, call rpcCall) rpcReply {
	switch call.Method {
	case "ListTree":
		return rpcReply{ID: call.ID, Result: b.ListTree(ctx)}
	case "ListSessions":
		return rpcReply{ID: call.ID, Result: b.ListSessions()}
	case "Search":
		var q app.SearchQuery
		if len(call.Params) > 0 {
			if err := json.Unmarshal(call.Params, &q); err != nil {
				return rpcReply{ID: call.ID, Error: err.Error()}
			}
		}
		return rpcReply{ID: call.ID, Result: b.Search(ctx, q)}
	default:
		return rpcReply{ID: call.ID, Error: "unknown method: " + call.Method}
	}
}
