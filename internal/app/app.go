package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	credhost "github.com/darkace1998/GoRemote/host/credential"
	pluginhost "github.com/darkace1998/GoRemote/host/plugin"
	protohost "github.com/darkace1998/GoRemote/host/protocol"
	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/internal/eventbus"
	"github.com/darkace1998/GoRemote/internal/logging"
	"github.com/darkace1998/GoRemote/internal/persistence"
	"github.com/darkace1998/GoRemote/sdk/credential"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// DefaultPersistInterval is the debounce interval used by the background
// persister goroutine: at most one flush per interval when the tree is dirty.
const DefaultPersistInterval = 500 * time.Millisecond

// Config configures the application core.
type Config struct {
	// Dir is the on-disk storage directory for persistence. Required.
	Dir string
	// Logger receives structured logs. Defaults to the logging package's
	// JSON logger on stderr.
	Logger *slog.Logger
	// Clock is the time source; defaults to time.Now.
	Clock func() time.Time
	// PersistInterval overrides DefaultPersistInterval for tests.
	PersistInterval time.Duration

	// PluginHost, ProtocolHost, CredentialHost are optional dependency-
	// injection slots. When nil, the app constructs its own. If any of
	// ProtocolHost or CredentialHost is provided, PluginHost should be
	// provided too so they share the same underlying plugin registry.
	PluginHost     *pluginhost.Host
	ProtocolHost   *protohost.Host
	CredentialHost *credhost.Host
}

// App is the cohesive application core: domain tree + persistence + plugin
// hosts + event bus + session manager.
//
// App is safe for concurrent use. All mutating commands take the internal
// write lock; reads use the read lock. Tree mutations mark the app dirty
// and are flushed to disk by a background goroutine on a debounce timer.
type App struct {
	cfg    Config
	logger *slog.Logger
	now    func() time.Time

	store *persistence.Store

	treeMu    sync.RWMutex
	tree      *domain.Tree
	templates []domain.ConnectionTemplate
	workspace domain.WorkspaceLayout
	meta      persistence.Meta

	pluginBus *eventbus.Bus[pluginhost.Event]
	pluginH   *pluginhost.Host
	protoH    *protohost.Host
	credH     *credhost.Host

	events *eventbus.Bus[Event]

	sess *sessionManager

	rootCtx    context.Context
	rootCancel context.CancelFunc

	// persister state
	persistMu   sync.Mutex
	dirty       atomic.Bool
	persistSig  chan struct{}
	persistDone chan struct{}
	started     atomic.Bool
	stopped     atomic.Bool
}

// New constructs an App. It loads the on-disk snapshot (or creates an empty
// one), wires the hosts, and prepares the session manager. It does NOT start
// the background persister — call Start for that.
func New(cfg Config) (*App, error) {
	if cfg.Dir == "" {
		return nil, errors.New("app: Config.Dir is required")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.PersistInterval <= 0 {
		cfg.PersistInterval = DefaultPersistInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.New(logging.Options{})
	}
	logger := logging.WithComponent(cfg.Logger, "app")

	store := persistence.New(cfg.Dir)

	loadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := store.Load(loadCtx)
	if err != nil {
		return nil, fmt.Errorf("app: load snapshot: %w", err)
	}
	if snap.Tree == nil {
		snap.Tree = domain.NewTree()
	}

	pluginBus := eventbus.New[pluginhost.Event]()
	ph := cfg.PluginHost
	if ph == nil {
		ph = pluginhost.New(pluginBus)
	}
	proto := cfg.ProtocolHost
	if proto == nil {
		proto = protohost.New(ph)
	}
	cred := cfg.CredentialHost
	if cred == nil {
		cred = credhost.New(ph, credhost.WithAuditLogger(logger))
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())

	a := &App{
		cfg:         cfg,
		logger:      logger,
		now:         cfg.Clock,
		store:       store,
		tree:        snap.Tree,
		templates:   snap.Templates,
		workspace:   snap.Workspace,
		meta:        snap.Meta,
		pluginBus:   pluginBus,
		pluginH:     ph,
		protoH:      proto,
		credH:       cred,
		events:      eventbus.New[Event](),
		rootCtx:     rootCtx,
		rootCancel:  rootCancel,
		persistSig:  make(chan struct{}, 1),
		persistDone: make(chan struct{}),
	}
	a.sess = newSessionManager(a)
	return a, nil
}

// Logger returns the app logger.
func (a *App) Logger() *slog.Logger { return a.logger }

// Store returns the persistence store.
func (a *App) Store() *persistence.Store { return a.store }

// PluginHost returns the generic plugin host.
func (a *App) PluginHost() *pluginhost.Host { return a.pluginH }

// ProtocolHost returns the protocol-module host.
func (a *App) ProtocolHost() *protohost.Host { return a.protoH }

// CredentialHost returns the credential-provider host.
func (a *App) CredentialHost() *credhost.Host { return a.credH }

// PluginEvents returns the plugin-host event bus.
func (a *App) PluginEvents() *eventbus.Bus[pluginhost.Event] { return a.pluginBus }

// RegisterProtocol registers a protocol module through the protocol host.
// Intended for built-in protocols wired at startup.
func (a *App) RegisterProtocol(ctx context.Context, m protocol.Module, trust sdkplugin.Trust) error {
	return a.protoH.Register(ctx, m, trust)
}

// RegisterCredential registers a credential provider through the credential
// host. Intended for built-in providers wired at startup.
func (a *App) RegisterCredential(ctx context.Context, p credential.Provider, trust sdkplugin.Trust) error {
	return a.credH.Register(ctx, p, trust)
}

// Start activates the background persister. It is safe to call once. Calling
// Start on an already-started App is a no-op.
func (a *App) Start(ctx context.Context) error {
	if !a.started.CompareAndSwap(false, true) {
		return nil
	}
	go a.persisterLoop()
	a.logger.Info("app started", slog.String("dir", a.cfg.Dir))
	return nil
}

// Shutdown closes all active sessions, stops the persister (flushing any
// pending mutations), and unregisters every plugin. Safe to call once.
func (a *App) Shutdown(ctx context.Context) error {
	if !a.stopped.CompareAndSwap(false, true) {
		return nil
	}
	// 1. Close active sessions first so plugin.Shutdown does not race
	//    with in-flight Start goroutines.
	a.sess.closeAll(ctx)

	// 2. Stop the persister and flush synchronously.
	if a.started.Load() {
		a.rootCancel()
		close(a.persistSig)
		<-a.persistDone
	} else {
		a.rootCancel()
	}
	if err := a.flushNow(ctx); err != nil {
		a.logger.Error("final flush failed", slog.String("err", err.Error()))
	}

	// 3. Unregister every plugin (triggers Lifecycle.Shutdown).
	for _, l := range a.pluginH.List() {
		if err := a.pluginH.Unregister(ctx, l.Manifest.ID); err != nil {
			a.logger.Warn("plugin unregister failed",
				slog.String("plugin", l.Manifest.ID),
				slog.String("err", err.Error()))
		}
	}

	// 4. Close buses last so late shutdown logs can still fire events.
	a.events.Close()
	a.pluginBus.Close()
	return nil
}

// markDirty signals the persister that a tree mutation has occurred. Called
// under the write lock from command handlers.
func (a *App) markDirty() {
	a.dirty.Store(true)
	if !a.started.Load() {
		return
	}
	select {
	case a.persistSig <- struct{}{}:
	default:
	}
}

// flushNow writes the current snapshot to disk synchronously, regardless of
// the dirty flag.
func (a *App) flushNow(ctx context.Context) error {
	a.persistMu.Lock()
	defer a.persistMu.Unlock()
	return a.flushNowLocked(ctx)
}

// flushNowLocked writes the current snapshot to disk. a.persistMu must be held.
func (a *App) flushNowLocked(ctx context.Context) error {
	a.treeMu.RLock()
	snap := &persistence.Snapshot{
		Tree:      a.tree,
		Templates: append([]domain.ConnectionTemplate(nil), a.templates...),
		Workspace: a.workspace,
		Meta:      a.meta,
	}
	a.treeMu.RUnlock()
	if err := a.store.Save(ctx, snap); err != nil {
		return err
	}
	a.treeMu.Lock()
	a.meta = snap.Meta
	a.treeMu.Unlock()
	a.dirty.Store(false)
	return nil
}
