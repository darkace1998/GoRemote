package persistence

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultBackupRetention is the maximum number of zip backups Store.Backup
// retains under <dir>/backups/. Older backups beyond this count are pruned
// (oldest first) after each new backup is written.
const DefaultBackupRetention = 10

// backupTimestampLayout is the filename timestamp format used for backups.
// It sorts lexicographically in the same order as chronologically.
const backupTimestampLayout = "20060102T150405Z"

// Backup writes a zip archive of the current store contents under
// <dir>/backups/<timestamp>.zip and returns the archive path. Contents of
// the backups/ directory itself are excluded from the archive. After a
// successful write, backups older than DefaultBackupRetention are pruned.
func (s *Store) Backup(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.backupLocked(ctx)
}

// backupLocked is the caller-locked implementation of Backup. It must be
// called while s.mu is held (read or write). It writes the new archive,
// then prunes according to DefaultBackupRetention.
func (s *Store) backupLocked(ctx context.Context) (string, error) {
	backupsDir := filepath.Join(s.dir, BackupsDirName)
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return "", fmt.Errorf("persistence: mkdir backups: %w", err)
	}

	ts := s.now().UTC().Format(backupTimestampLayout)
	// Ensure uniqueness even if two backups land in the same second.
	path := filepath.Join(backupsDir, ts+".zip")
	for i := 1; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return "", err
		}
		path = filepath.Join(backupsDir, fmt.Sprintf("%s-%d.zip", ts, i))
	}

	if err := zipStoreDir(ctx, s.dir, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}

	if err := pruneBackups(backupsDir, DefaultBackupRetention); err != nil {
		// Pruning is best-effort; surface the error but keep the archive.
		return path, err
	}
	return path, nil
}

// zipStoreDir writes every regular file under dir (excluding the top-level
// BackupsDirName directory) to outPath as a zip archive. Paths inside the
// zip are relative to dir and use forward slashes.
func zipStoreDir(ctx context.Context, dir, outPath string) error {
	// #nosec G304 -- outPath is generated under the store-managed backups directory.
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("persistence: create backup: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		// Skip the backups subtree.
		first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if first == BackupsDirName {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		zipName := filepath.ToSlash(rel)
		hdr, hErr := zip.FileInfoHeader(info)
		if hErr != nil {
			return hErr
		}
		hdr.Name = zipName
		hdr.Method = zip.Deflate
		w, zerr := zw.CreateHeader(hdr)
		if zerr != nil {
			return zerr
		}
		// #nosec G304 -- path comes directly from filepath.Walk over the store root.
		f, ferr := os.Open(path)
		if ferr != nil {
			return ferr
		}
		_, cpErr := io.Copy(w, f)
		_ = f.Close()
		return cpErr
	})
	if walkErr != nil {
		_ = zw.Close()
		return walkErr
	}
	if cerr := zw.Close(); cerr != nil {
		return cerr
	}
	if serr := out.Sync(); serr != nil {
		return serr
	}
	return nil
}

// pruneBackups deletes the oldest *.zip files in backupsDir so that at most
// keep files remain. Non-zip entries are ignored.
func pruneBackups(backupsDir string, keep int) error {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return err
	}
	type candidate struct {
		name    string
		modTime time.Time
	}
	var zips []candidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			return ierr
		}
		zips = append(zips, candidate{name: e.Name(), modTime: info.ModTime()})
	}
	if len(zips) <= keep {
		return nil
	}
	sort.Slice(zips, func(i, j int) bool {
		if !zips[i].modTime.Equal(zips[j].modTime) {
			return zips[i].modTime.Before(zips[j].modTime)
		}
		return zips[i].name < zips[j].name
	})
	drop := len(zips) - keep
	for _, c := range zips[:drop] {
		if err := os.Remove(filepath.Join(backupsDir, c.name)); err != nil {
			return err
		}
	}
	return nil
}

// Restore extracts backupPath over the current store directory. Before any
// destructive change it captures a fresh safety-backup of the current state
// into <dir>/backups/ so an operator can always roll back. The safety
// backup is retained under the same retention policy as manual backups.
//
// The backup archive is expected to be a zip produced by Backup (paths
// relative to the store root, no absolute or traversal-escaping entries).
// Restore refuses archives containing "../" components.
func (s *Store) Restore(ctx context.Context, backupPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.backupLocked(ctx); err != nil {
		return fmt.Errorf("persistence: pre-restore safety backup: %w", err)
	}

	// Validate archive before touching the existing files.
	// #nosec G304 -- backupPath is an explicit user-selected restore source.
	zr, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("persistence: open backup %s: %w", backupPath, err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		clean, err := sanitizeRestorePath(f.Name)
		if err != nil {
			return fmt.Errorf("persistence: backup contains unsafe path %q", f.Name)
		}
		if strings.HasPrefix(clean, BackupsDirName+"/") || clean == BackupsDirName {
			return fmt.Errorf("persistence: backup contains nested backups/ entry %q", f.Name)
		}
	}

	// Remove current top-level files/dirs except backups/.
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("persistence: read dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == BackupsDirName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.dir, e.Name())); err != nil {
			return fmt.Errorf("persistence: clear %s: %w", e.Name(), err)
		}
	}

	// Extract.
	for _, f := range zr.File {
		if err := extractZipEntry(f, s.dir); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeRestorePath(name string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == "" || clean == "/" {
		return "", errors.New("empty restore path")
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.HasPrefix(clean, "/") {
		return "", errors.New("path escapes root")
	}
	return clean, nil
}

// extractZipEntry extracts a single zip entry relative to root. Directories
// are created with 0o700 and files with 0o600 permissions.
func extractZipEntry(f *zip.File, root string) error {
	clean, err := sanitizeRestorePath(f.Name)
	if err != nil {
		return fmt.Errorf("persistence: backup contains unsafe path %q", f.Name)
	}
	dest, err := safeJoinWithinRoot(root, filepath.FromSlash(clean))
	if err != nil {
		return fmt.Errorf("persistence: backup contains unsafe path %q", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(dest, 0o700)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	// #nosec G304 -- dest is constrained to root by safeJoinWithinRoot.
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func safeJoinWithinRoot(root, rel string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(base, rel)
	dest, err = filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(base, dest)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root")
	}
	return dest, nil
}
