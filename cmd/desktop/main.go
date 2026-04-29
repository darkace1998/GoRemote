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

	"github.com/darkace1998/GoRemote/app/extplugin"
	"github.com/darkace1998/GoRemote/app/settings"
	gosync "github.com/darkace1998/GoRemote/app/sync"
	"github.com/darkace1998/GoRemote/app/update"
	"github.com/darkace1998/GoRemote/app/workspace"
	pluginhost "github.com/darkace1998/GoRemote/host/plugin"
	"github.com/darkace1998/GoRemote/internal/app"
	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/internal/logging"
	"github.com/darkace1998/GoRemote/internal/platform"
	sdkcred "github.com/darkace1998/GoRemote/sdk/credential"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
	protocol "github.com/darkace1998/GoRemote/sdk/protocol"

	credbw "github.com/darkace1998/GoRemote/plugins/credential-bitwarden"
	credfile "github.com/darkace1998/GoRemote/plugins/credential-file"
	credkeychain "github.com/darkace1998/GoRemote/plugins/credential-keychain"
	protohttp "github.com/darkace1998/GoRemote/plugins/protocol-http"
	protomosh "github.com/darkace1998/GoRemote/plugins/protocol-mosh"
	protopowershell "github.com/darkace1998/GoRemote/plugins/protocol-powershell"
	protoraw "github.com/darkace1998/GoRemote/plugins/protocol-rawsocket"
	protordp "github.com/darkace1998/GoRemote/plugins/protocol-rdp"
	protorlogin "github.com/darkace1998/GoRemote/plugins/protocol-rlogin"
	protoserial "github.com/darkace1998/GoRemote/plugins/protocol-serial"
	protosftp "github.com/darkace1998/GoRemote/plugins/protocol-sftp"
	protossh "github.com/darkace1998/GoRemote/plugins/protocol-ssh"
	prototelnet "github.com/darkace1998/GoRemote/plugins/protocol-telnet"
	prototn5250 "github.com/darkace1998/GoRemote/plugins/protocol-tn5250"
	protovnc "github.com/darkace1998/GoRemote/plugins/protocol-vnc"
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
	logLevel  *slog.LevelVar
	stateDir  string
	logPath   string
	pluginReg *extplugin.Registry
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

// WithLogLevelVar attaches a runtime-mutable log level to the bindings so the
// settings UI can change verbosity without restarting the application.
func (b *Bindings) WithLogLevelVar(v *slog.LevelVar) *Bindings {
	b.logLevel = v
	return b
}

// WithStateDir records the on-disk directory the app stores its
// configuration / inventory in. It is used by optional features like
// the git sync backend to know what directory to mirror.
func (b *Bindings) WithStateDir(dir string) *Bindings {
	b.stateDir = dir
	return b
}

// WithPluginRegistry attaches the external-plugin registry. May be nil
// (e.g. when the registry could not be opened); UI code must guard.
func (b *Bindings) WithPluginRegistry(r *extplugin.Registry) *Bindings {
	b.pluginReg = r
	return b
}

// PluginRegistry returns the attached external-plugin registry or nil.
func (b *Bindings) PluginRegistry() *extplugin.Registry { return b.pluginReg }

// WithLogPath records the active log file path so the in-app log viewer
// and diagnostic bundle can read it.
func (b *Bindings) WithLogPath(p string) *Bindings {
	b.logPath = p
	return b
}

// LogPath returns the active log file path or "" when not configured.
func (b *Bindings) LogPath() string { return b.logPath }

// StateDir returns the application state directory or "".
func (b *Bindings) StateDir() string { return b.stateDir }

// SetLogLevel updates the active runtime log level. Recognised names are
// "trace", "debug", "info", "warn", "error" (case-insensitive). An unknown
// value falls back to "info".
func (b *Bindings) SetLogLevel(level string) {
	if b.logLevel == nil {
		return
	}
	b.logLevel.Set(resolveLogLevel(level))
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
	logging.Trace(b.logger, "tree.CreateFolder",
		slog.String("parent", parent),
		slog.String("name", name),
		slog.Int("tags", len(tags)),
	)
	id, err := b.app.CreateFolder(ctx, pid, name, app.FolderOpts{Description: description, Tags: tags})
	if err != nil {
		b.logger.Debug("tree.CreateFolder failed",
			slog.String("parent", parent),
			slog.String("name", name),
			slog.String("err", err.Error()),
		)
		return "", err
	}
	b.logger.Debug("tree.CreateFolder ok",
		slog.String("parent", parent),
		slog.String("name", name),
		slog.String("id", id.String()),
	)
	return id.String(), nil
}

// CreateConnection creates a connection under parent.
func (b *Bindings) CreateConnection(ctx context.Context, parent string, opts ConnectionInput) (string, error) {
	pid, err := parseID(parent)
	if err != nil {
		return "", err
	}
	logging.Trace(b.logger, "tree.CreateConnection",
		slog.String("parent", parent),
		slog.String("name", opts.Name),
		slog.String("protocol", opts.ProtocolID),
		slog.String("host", opts.Host),
		slog.Int("port", opts.Port),
	)
	id, err := b.app.CreateConnection(ctx, pid, opts.toAppOpts())
	if err != nil {
		b.logger.Debug("tree.CreateConnection failed",
			slog.String("parent", parent),
			slog.String("name", opts.Name),
			slog.String("err", err.Error()),
		)
		return "", err
	}
	b.logger.Debug("tree.CreateConnection ok",
		slog.String("parent", parent),
		slog.String("id", id.String()),
		slog.String("name", opts.Name),
	)
	return id.String(), nil
}

// UpdateConnection applies a patch to an existing connection.
func (b *Bindings) UpdateConnection(ctx context.Context, id string, patch ConnectionPatchInput) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	logging.Trace(b.logger, "tree.UpdateConnection",
		slog.String("id", id),
		slog.Bool("name_set", patch.Name != nil),
		slog.Bool("host_set", patch.Host != nil),
		slog.Bool("protocol_set", patch.ProtocolID != nil),
	)
	if err := b.app.UpdateConnection(ctx, nid, patch.toAppPatch()); err != nil {
		b.logger.Debug("tree.UpdateConnection failed", slog.String("id", id), slog.String("err", err.Error()))
		return err
	}
	b.logger.Debug("tree.UpdateConnection ok", slog.String("id", id))
	return nil
}

// UpdateFolder applies a patch to an existing folder.
func (b *Bindings) UpdateFolder(ctx context.Context, id string, patch FolderPatchInput) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	logging.Trace(b.logger, "tree.UpdateFolder",
		slog.String("id", id),
		slog.Bool("name_set", patch.Name != nil),
	)
	if err := b.app.UpdateFolder(ctx, nid, patch.toAppPatch()); err != nil {
		b.logger.Debug("tree.UpdateFolder failed", slog.String("id", id), slog.String("err", err.Error()))
		return err
	}
	b.logger.Debug("tree.UpdateFolder ok", slog.String("id", id))
	return nil
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
	logging.Trace(b.logger, "tree.MoveNode",
		slog.String("id", id),
		slog.String("new_parent", newParent),
	)
	if err := b.app.MoveNode(ctx, nid, pid); err != nil {
		b.logger.Debug("tree.MoveNode failed",
			slog.String("id", id),
			slog.String("new_parent", newParent),
			slog.String("err", err.Error()),
		)
		return err
	}
	b.logger.Debug("tree.MoveNode ok",
		slog.String("id", id),
		slog.String("new_parent", newParent),
	)
	return nil
}

// DeleteNode removes a folder or connection.
func (b *Bindings) DeleteNode(ctx context.Context, id string) error {
	nid, err := parseID(id)
	if err != nil {
		return err
	}
	logging.Trace(b.logger, "tree.DeleteNode", slog.String("id", id))
	if err := b.app.DeleteNode(ctx, nid); err != nil {
		b.logger.Debug("tree.DeleteNode failed", slog.String("id", id), slog.String("err", err.Error()))
		return err
	}
	b.logger.Debug("tree.DeleteNode ok", slog.String("id", id))
	return nil
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

// ToggleFavorite flips the favorite flag on a connection. Returns the new
// state. Errors when the id does not resolve to a connection.
func (b *Bindings) ToggleFavorite(ctx context.Context, id string) (bool, error) {
	nid, err := parseID(id)
	if err != nil {
		return false, err
	}
	logging.Trace(b.logger, "tree.ToggleFavorite", slog.String("id", id))
	state, err := b.app.ToggleFavorite(ctx, nid)
	if err != nil {
		b.logger.Debug("tree.ToggleFavorite failed", slog.String("id", id), slog.String("err", err.Error()))
		return false, err
	}
	b.logger.Debug("tree.ToggleFavorite ok", slog.String("id", id), slog.Bool("favorite", state))
	return state, nil
}

// ListFavorites returns every connection whose Favorite flag is set.
func (b *Bindings) ListFavorites(ctx context.Context) []*app.NodeView {
	return b.app.ListFavorites(ctx)
}

// TouchRecent records a fresh open of the given connection in the
// workspace's recents list. Best-effort: errors are logged but not
// returned to the caller (a missing recent must never block opening
// a session).
func (b *Bindings) TouchRecent(ctx context.Context, connectionID string) {
	if b.workspace == nil || connectionID == "" {
		return
	}
	w, err := b.workspace.Load(ctx)
	if err != nil {
		b.logger.Debug("recents: load failed", slog.String("err", err.Error()))
		return
	}
	w.TouchRecent(connectionID, time.Now())
	if err := b.workspace.Save(ctx, w); err != nil {
		b.logger.Debug("recents: save failed", slog.String("err", err.Error()))
	}
}

// ListRecents returns the workspace's recents list (most-recent-first,
// bounded to workspace.MaxRecents). Connection IDs that no longer
// resolve are filtered out.
func (b *Bindings) ListRecents(ctx context.Context) []app.NodeView {
	if b.workspace == nil {
		return nil
	}
	w, err := b.workspace.Load(ctx)
	if err != nil || len(w.Recents) == 0 {
		return nil
	}
	out := make([]app.NodeView, 0, len(w.Recents))
	for _, r := range w.Recents {
		nid, err := parseID(r.ConnectionID)
		if err != nil {
			continue
		}
		view, err := b.app.GetConnection(ctx, nid)
		if err != nil {
			continue
		}
		out = append(out, app.NodeView{
			ID:          view.ID,
			Kind:        "connection",
			Name:        view.Name,
			ParentID:    view.ParentID,
			Description: view.Description,
			Tags:        view.Tags,
			Icon:        view.Icon,
			Color:       view.Color,
			Environment: view.Environment,
			Favorite:    view.Favorite,
			Protocol:    view.Protocol,
			Host:        view.Host,
			Port:        view.Port,
			Username:    view.Username,
		})
	}
	return out
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
	b.TouchRecent(ctx, connectionID)
	return h.String(), nil
}

// OpenSessionWithPassword starts a session using a one-shot inline password
// (and optional username override). The password is not persisted.
func (b *Bindings) OpenSessionWithPassword(ctx context.Context, connectionID, username, password string) (string, error) {
	nid, err := parseID(connectionID)
	if err != nil {
		return "", err
	}
	mat := &sdkcred.Material{Username: username, Password: password}
	h, err := b.app.OpenSessionWithSecret(ctx, nid, mat)
	if err != nil {
		return "", err
	}
	b.TouchRecent(ctx, connectionID)
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
	// #nosec G304 -- path is an explicit user-selected import source.
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
//
// On a successful local save, if the user has enabled git sync in their
// settings, the workspace directory is committed and (best-effort)
// pushed to the configured remote. Sync failures are logged but never
// returned — they must not block the UI.
func (b *Bindings) SaveWorkspace(ctx context.Context, w workspace.Workspace) error {
	if b.workspace == nil {
		return errors.New("workspace store not initialised")
	}
	if err := b.workspace.Save(ctx, w); err != nil {
		return err
	}
	go b.maybeGitSync(ctx, "workspace updated")
	return nil
}

// maybeGitSync runs the git-sync backend if the user has enabled it.
// Best-effort: any failure is logged but never propagates. Runs on its
// own context with a fresh timeout so it does not extend the caller's
// save deadline.
func (b *Bindings) maybeGitSync(parent context.Context, msg string) {
	if b.stateDir == "" || b.settings == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 60*time.Second)
	defer cancel()
	s, err := b.settings.Get(ctx)
	if err != nil || !s.GitSyncEnabled {
		return
	}
	g, err := gosync.New(gosync.Config{Dir: b.stateDir, Remote: s.GitSyncRemote, Branch: s.GitSyncBranch})
	if err != nil {
		b.logger.Warn("git sync: new", slog.String("err", err.Error()))
		return
	}
	if !g.Available() {
		b.logger.Warn("git sync: git binary not available on PATH")
		return
	}
	if err := g.CommitAndPush(ctx, msg); err != nil {
		b.logger.Warn("git sync: commit/push", slog.String("err", err.Error()))
		return
	}
	b.logger.Info("git sync: ok")
}

// SyncNow runs the git sync backend immediately, regardless of the
// enabled flag. The flag still controls whether saves auto-trigger
// sync; this method is the explicit "Sync now" toolbar action.
func (b *Bindings) SyncNow(ctx context.Context) error {
	if b.stateDir == "" {
		return errors.New("state directory not configured")
	}
	if b.settings == nil {
		return errors.New("settings store not configured")
	}
	s, err := b.settings.Get(ctx)
	if err != nil {
		return err
	}
	g, err := gosync.New(gosync.Config{Dir: b.stateDir, Remote: s.GitSyncRemote, Branch: s.GitSyncBranch})
	if err != nil {
		return err
	}
	if !g.Available() {
		return errors.New("git binary not available on PATH")
	}
	return g.CommitAndPush(ctx, "goremote: manual sync")
}

// UpdateInfo summarises the result of CheckForUpdate for the UI.
type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Notes          string `json:"notes,omitempty"`
}

// CheckForUpdate fetches the configured update manifest, verifies the
// signature against the configured public key, and reports whether a
// newer version is available. Returns an error when settings are
// missing/invalid; an "available=false" result is never an error.
func (b *Bindings) CheckForUpdate(ctx context.Context) (UpdateInfo, error) {
	if b.settings == nil {
		return UpdateInfo{}, errors.New("settings store not configured")
	}
	s, err := b.settings.Get(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	if s.AutoUpdateURL == "" || s.AutoUpdatePublicKey == "" {
		return UpdateInfo{}, errors.New("auto-update URL or public key not configured")
	}
	m, err := update.FetchManifest(ctx, s.AutoUpdateURL)
	if err != nil {
		return UpdateInfo{}, err
	}
	tgt, err := m.SelectTarget()
	if err != nil {
		return UpdateInfo{}, err
	}
	if err := tgt.VerifySignature(m.Version, s.AutoUpdatePublicKey); err != nil {
		return UpdateInfo{}, err
	}
	current := strings.TrimSpace(currentAppVersion())
	out := UpdateInfo{CurrentVersion: current, LatestVersion: m.Version, Notes: m.Notes}
	out.Available = update.IsNewer(m.Version, current)
	return out, nil
}

// ApplyUpdate downloads the verified target and replaces the running
// binary in place. The caller is expected to inform the user to
// restart the application after this returns nil.
func (b *Bindings) ApplyUpdate(ctx context.Context) error {
	if b.settings == nil {
		return errors.New("settings store not configured")
	}
	s, err := b.settings.Get(ctx)
	if err != nil {
		return err
	}
	if s.AutoUpdateURL == "" || s.AutoUpdatePublicKey == "" {
		return errors.New("auto-update URL or public key not configured")
	}
	m, err := update.FetchManifest(ctx, s.AutoUpdateURL)
	if err != nil {
		return err
	}
	tgt, err := m.SelectTarget()
	if err != nil {
		return err
	}
	if err := tgt.VerifySignature(m.Version, s.AutoUpdatePublicKey); err != nil {
		return err
	}
	tmpDir := os.TempDir()
	if b.stateDir != "" {
		tmpDir = filepath.Join(b.stateDir, "updates")
	}
	path, err := update.Download(ctx, tgt, tmpDir)
	if err != nil {
		return err
	}
	if err := update.SwapInPlace(path); err != nil {
		return err
	}
	b.logger.Info("update: applied", slog.String("version", m.Version))
	return nil
}

// currentAppVersion is overridden in main.go via the linker (-X
// main.Version). Keeping it as a function lets the bindings stay
// independent of package-level state.
func currentAppVersion() string {
	if Version == "" {
		return "0.0.0"
	}
	return Version
}

// --- Credential providers ----------------------------------------------

// CredentialProviderInfo is a UI-facing snapshot of a registered credential
// provider, including its current lock/unlock state.
type CredentialProviderInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// ListCredentialProviders returns one entry per registered provider with
// its manifest name and current state ("unlocked", "locked", "unavailable",
// "ready"). The credential host is consulted lazily so this is safe even
// when the host is misconfigured.
func (b *Bindings) ListCredentialProviders(ctx context.Context) []CredentialProviderInfo {
	if b.app == nil || b.app.CredentialHost() == nil {
		return nil
	}
	ch := b.app.CredentialHost()
	provs := ch.List()
	out := make([]CredentialProviderInfo, 0, len(provs))
	for _, p := range provs {
		m := p.Manifest()
		st := ch.State(ctx, m.ID)
		out = append(out, CredentialProviderInfo{
			ID:    m.ID,
			Name:  m.Name,
			State: string(st),
		})
	}
	return out
}

// UnlockCredentialProvider unlocks the provider identified by id using the
// supplied passphrase. The 30-second timeout matches the host-default and
// gives the Bitwarden CLI room to talk to a remote server.
func (b *Bindings) UnlockCredentialProvider(ctx context.Context, id, passphrase string) error {
	if b.app == nil || b.app.CredentialHost() == nil {
		return errors.New("credential host not initialised")
	}
	logging.Trace(b.logger, "credential.Unlock", slog.String("provider", id))
	if err := b.app.CredentialHost().Unlock(ctx, id, passphrase, 30*time.Second); err != nil {
		b.logger.Error("credential unlock failed",
			slog.String("provider", id),
			slog.String("err", err.Error()))
		return err
	}
	b.logger.Info("credential provider unlocked", slog.String("provider", id))
	return nil
}

// LockCredentialProvider locks the provider identified by id. Idempotent
// for providers that are already locked.
func (b *Bindings) LockCredentialProvider(ctx context.Context, id string) error {
	if b.app == nil || b.app.CredentialHost() == nil {
		return errors.New("credential host not initialised")
	}
	logging.Trace(b.logger, "credential.Lock", slog.String("provider", id))
	if err := b.app.CredentialHost().Lock(ctx, id, 10*time.Second); err != nil {
		b.logger.Error("credential lock failed",
			slog.String("provider", id),
			slog.String("err", err.Error()))
		return err
	}
	b.logger.Info("credential provider locked", slog.String("provider", id))
	return nil
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
	AuthMethod    string             `json:"authMethod"`
	CredentialRef CredentialRefInput `json:"credentialRef"`
	Description   string             `json:"description"`
	Tags          []string           `json:"tags"`
	Settings      map[string]any     `json:"settings"`
	Icon          string             `json:"icon,omitempty"`
	Color         string             `json:"color,omitempty"`
	Environment   string             `json:"environment"`
	Favorite      bool               `json:"favorite,omitempty"`
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
		AuthMethod:    protocol.AuthMethod(in.AuthMethod),
		CredentialRef: credRefFromInput(in.CredentialRef),
		Description:   in.Description,
		Tags:          in.Tags,
		Settings:      in.Settings,
		Icon:          in.Icon,
		Color:         in.Color,
		Environment:   in.Environment,
		Favorite:      in.Favorite,
	}
}

// ConnectionPatchInput is the JSON form of app.ConnectionPatch.
type ConnectionPatchInput struct {
	Name          *string             `json:"name,omitempty"`
	ProtocolID    *string             `json:"protocolId,omitempty"`
	Host          *string             `json:"host,omitempty"`
	Port          *int                `json:"port,omitempty"`
	Username      *string             `json:"username,omitempty"`
	AuthMethod    *string             `json:"authMethod,omitempty"`
	CredentialRef *CredentialRefInput `json:"credentialRef,omitempty"`
	Description   *string             `json:"description,omitempty"`
	Tags          *[]string           `json:"tags,omitempty"`
	Settings      *map[string]any     `json:"settings,omitempty"`
	Icon          *string             `json:"icon,omitempty"`
	Color         *string             `json:"color,omitempty"`
	Environment   *string             `json:"environment,omitempty"`
	Favorite      *bool               `json:"favorite,omitempty"`
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
		Icon:        p.Icon,
		Color:       p.Color,
		Environment: p.Environment,
		Favorite:    p.Favorite,
	}
	if p.AuthMethod != nil {
		am := protocol.AuthMethod(*p.AuthMethod)
		out.AuthMethod = &am
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

// crashState holds the directory used by the global crash dumper so the
// top-level recover() can write a crash file even when the panic happens
// long after main() set up the rest of the world.
var crashState struct {
	dir     string
	enabled bool
}

func main() {
	defer dumpCrashIfPanicking()

	levelVar := new(slog.LevelVar)
	levelVar.Set(selectLogLevel(os.Getenv("GOREMOTE_LOG_LEVEL"), loadConfiguredLogLevel()))

	dir, err := resolveStateDir()
	if err != nil {
		// We don't have a logger yet; fall back to stderr.
		fmt.Fprintf(os.Stderr, "resolve state dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "create state dir: %v\n", err)
		os.Exit(1)
	}
	crashState.dir = dir
	crashState.enabled = true // default on; flipped after Settings load

	// Build the logger writer: stderr (always) plus a rotating file
	// under <state>/logs/goremote.log. Failures to open the file sink
	// degrade to stderr-only logging.
	logWriter := chooseLogWriter(runtime.GOOS, os.Stderr, os.Stdout)
	logFilePath := filepath.Join(dir, "logs", "goremote.log")
	var fileSink *logging.FileSink
	if fs, ferr := logging.OpenFileSink(logFilePath, 0); ferr != nil {
		fmt.Fprintf(os.Stderr, "log file sink: %v\n", ferr)
	} else {
		fileSink = fs
		logWriter = logging.MultiWriter(logWriter, fs)
	}
	logger := logging.New(logging.Options{
		Writer: logWriter,
		Level:  levelVar,
	})
	slog.SetDefault(logger)
	if fileSink != nil {
		defer func() { _ = fileSink.Close() }()
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

	bindings := NewBindings(a).WithLogLevelVar(levelVar).WithStateDir(dir).WithLogPath(logFilePath)

	// External plugin registry — best-effort. Failures degrade the
	// Plugins dialog to a read-only warning state.
	if reg, err := extplugin.Open(filepath.Join(dir, "plugins")); err != nil {
		logger.Warn("extplugin: open registry", slog.String("err", err.Error()))
	} else {
		bindings.WithPluginRegistry(reg)
	}

	// Best-effort: clean up any leftover .old executable from a prior
	// in-place update. Runs once, no error.
	update.CleanupOld()

	// Subscribe to the app event bus and log lifecycle events. Without
	// this, errors published as Event{Kind: EventError} (e.g. SSH auth
	// failures returned from protoH.Open) never reach the log file or
	// console — they are only visible in the GUI's error dialog.
	startEventLogger(a, logger)

	// Settings store. Keep failures non-fatal: the UI falls back to defaults.
	if sp, err := settings.DefaultPath(); err != nil {
		logger.Error("settings: resolve path", slog.String("err", err.Error()))
	} else {
		bindings.WithSettingsStore(settings.NewFileStoreWithLogger(sp, settings.NewSlogLogger(logger)))
	}

	// Honour the user's crash-report opt-out, if any. Default is on.
	if bindings.settings != nil {
		if s, err := bindings.settings.Get(context.Background()); err == nil {
			crashState.enabled = !s.CrashReportsDisabled
		}
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
		return stderr
	}
	return stderr
}

func resolveLogLevel(v string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "trace":
		return logging.LevelTrace
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

func selectLogLevel(envLevel, settingsLevel string) slog.Level {
	if strings.TrimSpace(envLevel) != "" {
		return resolveLogLevel(envLevel)
	}
	if strings.TrimSpace(settingsLevel) != "" {
		return resolveLogLevel(settingsLevel)
	}
	return slog.LevelInfo
}

func loadConfiguredLogLevel() string {
	sp, err := settings.DefaultPath()
	if err != nil {
		return ""
	}
	s, err := settings.NewFileStore(sp).Get(context.Background())
	if err != nil {
		return ""
	}
	return s.LogLevel
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
		return nil, fmt.Errorf("%w (quarantine failed: %w)", err, qerr)
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
		{"sftp", protosftp.New()},
		{"telnet", prototelnet.New()},
		{"rlogin", protorlogin.New()},
		{"rawsocket", protoraw.New()},
		{"http", protohttp.New()},
		{"powershell", protopowershell.New()},
		{"rdp", protordp.New()},
		{"vnc", protovnc.New()},
		{"tn5250", prototn5250.New()},
		{"mosh", protomosh.New()},
		{"serial", protoserial.New()},
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
		} else {
			return fmt.Errorf("register credential-keychain: %w", err)
		}
	}

	// Bitwarden CLI provider. The provider construction itself never fails:
	// when `bw` is missing it simply reports StateUnavailable. Allow the user
	// to override the binary path and (for self-hosted servers) the server
	// URL via env vars without needing a settings UI section yet.
	bwOpts := credbw.Options{
		BWBinary:  os.Getenv("GOREMOTE_BW_BINARY"),
		ServerURL: os.Getenv("GOREMOTE_BW_SERVER_URL"),
	}
	if err := a.RegisterCredential(ctx, credbw.New(bwOpts), sdkplugin.TrustCore); err != nil {
		if errors.Is(err, pluginhost.ErrPlatformUnsupported) {
			logger.Warn("credential provider unsupported on this platform; skipping",
				slog.String("provider", "credential-bitwarden"),
				slog.String("err", err.Error()))
		} else {
			return fmt.Errorf("register credential-bitwarden: %w", err)
		}
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

// startEventLogger spawns a goroutine that subscribes to the app event bus
// and emits a structured slog line for each lifecycle event. It runs for the
// process lifetime; the subscriber is detached when context.Background() is
// cancelled (i.e. never), so app shutdown closes it via Bus.Close().
func startEventLogger(a *app.App, logger *slog.Logger) {
	if a == nil || logger == nil {
		return
	}
	ch := a.Events().Subscribe(context.Background(), 64)
	elog := logger.With(slog.String("component", "events"))
	go func() {
		for ev := range ch {
			attrs := []any{
				slog.String("kind", string(ev.Kind)),
			}
			if ev.NodeID != domain.NilID {
				attrs = append(attrs, slog.String("node_id", ev.NodeID.String()))
			}
			if ev.NodeKind != "" {
				attrs = append(attrs, slog.String("node_kind", ev.NodeKind))
			}
			if ev.Name != "" {
				attrs = append(attrs, slog.String("name", ev.Name))
			}
			if ev.SessionID != domain.NilID {
				attrs = append(attrs, slog.String("session_id", ev.SessionID.String()))
			}
			if ev.ProviderID != "" {
				attrs = append(attrs, slog.String("provider_id", ev.ProviderID))
			}
			if ev.Where != "" {
				attrs = append(attrs, slog.String("where", ev.Where))
			}
			if ev.Err != nil {
				attrs = append(attrs, slog.String("err", ev.Err.Error()))
				elog.Error("app event", attrs...)
				continue
			}
			elog.Info("app event", attrs...)
		}
	}()
}
