// Package integration contains end-to-end integration tests for goremote's
// wired-up application core: app boot, plugin registry initialization,
// settings/workspace persistence, opening a connection through the protocol
// host with a fake protocol, applying a credential resolution, and clean
// shutdown. The tests in this package run entirely in-process; no real
// plugins are spawned and no network sockets are opened.
package integration

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/app/settings"
	"github.com/goremote/goremote/app/workspace"
	"github.com/goremote/goremote/internal/app"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"

	"github.com/goremote/goremote/test/integration/fakes/fakecred"
	"github.com/goremote/goremote/test/integration/fakes/fakeprotocol"
)

// Recorder bundles per-fake recorders so tests can reach them without
// reaching through the registry. The component recorders themselves are
// individually thread-safe (mutex-guarded slices); this aggregate is just
// a convenience handle.
type Recorder struct {
	Protocol *fakeprotocol.Recorder
	Cred     *fakecred.Recorder
}

// Harness owns a fully wired application core for a single test. Construct
// it with NewHarness; cleanup is registered via t.Cleanup.
type Harness struct {
	Dir       string
	App       *app.App
	Settings  settings.Store
	Workspace workspace.Store

	// Registries: goremote calls these "Hosts" but the public test surface
	// follows the spec's "Registry" naming for clarity.
	Protocols   ProtocolRegistry
	Credentials CredentialRegistry

	Protocol *fakeprotocol.Module
	Cred     *fakecred.Provider
	Recorder *Recorder

	shutdownOnce sync.Once
}

// ProtocolRegistry is a thin wrapper exposing the protocol host as a
// "registry" surface. It is implemented by *protohost.Host transparently.
type ProtocolRegistry interface {
	List() []protocolModule
}

// CredentialRegistry is the equivalent for credential providers.
type CredentialRegistry interface {
	List() []credentialProvider
}

// NewHarness boots an app core under a fresh t.TempDir(), registers the fake
// protocol + fake credential provider, and returns a Harness ready for use.
// All resources are torn down in t.Cleanup.
func NewHarness(t testing.TB) *Harness {
	t.Helper()
	return NewHarnessInDir(t, t.TempDir())
}

// NewHarnessInDir is like NewHarness but uses dir as the on-disk state
// directory instead of allocating a fresh one. Useful for reload tests.
func NewHarnessInDir(t testing.TB, dir string) *Harness {
	t.Helper()

	a, err := app.New(app.Config{Dir: dir})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	bootCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Start(bootCtx); err != nil {
		t.Fatalf("app.Start: %v", err)
	}

	proto := fakeprotocol.New()
	if err := a.RegisterProtocol(bootCtx, proto, sdkplugin.TrustCore); err != nil {
		_ = a.Shutdown(context.Background())
		t.Fatalf("RegisterProtocol: %v", err)
	}
	cred := fakecred.New()
	if err := a.RegisterCredential(bootCtx, cred, sdkplugin.TrustCore); err != nil {
		_ = a.Shutdown(context.Background())
		t.Fatalf("RegisterCredential: %v", err)
	}

	settingsStore := settings.NewFileStore(filepath.Join(dir, "ui", "settings.json"))
	wsStore := workspace.NewFileStore(filepath.Join(dir, "ui", "workspace.json"), nil)

	h := &Harness{
		Dir:         dir,
		App:         a,
		Settings:    settingsStore,
		Workspace:   wsStore,
		Protocols:   protocolRegistryAdapter{a: a},
		Credentials: credentialRegistryAdapter{a: a},
		Protocol:    proto,
		Cred:        cred,
		Recorder: &Recorder{
			Protocol: proto.Recorder(),
			Cred:     cred.Recorder(),
		},
	}

	t.Cleanup(func() {
		h.Shutdown(2 * time.Second)
	})
	return h
}

// Shutdown tears the harness down; safe to call multiple times.
func (h *Harness) Shutdown(timeout time.Duration) {
	h.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_ = h.App.Shutdown(ctx)
	})
}

// --- registry adapters ---------------------------------------------------

// protocolModule is the "registry list" element for protocols. It is a
// minimal projection of plugin.Manifest sufficient for tests.
type protocolModule struct {
	ID   string
	Name string
}

// credentialProvider mirrors protocolModule for credential providers.
type credentialProvider struct {
	ID   string
	Name string
}

type protocolRegistryAdapter struct{ a *app.App }

func (p protocolRegistryAdapter) List() []protocolModule {
	mods := p.a.ProtocolHost().List()
	out := make([]protocolModule, 0, len(mods))
	for _, m := range mods {
		man := m.Manifest()
		out = append(out, protocolModule{ID: man.ID, Name: man.Name})
	}
	return out
}

type credentialRegistryAdapter struct{ a *app.App }

func (c credentialRegistryAdapter) List() []credentialProvider {
	provs := c.a.CredentialHost().List()
	out := make([]credentialProvider, 0, len(provs))
	for _, p := range provs {
		man := p.Manifest()
		out = append(out, credentialProvider{ID: man.ID, Name: man.Name})
	}
	return out
}
