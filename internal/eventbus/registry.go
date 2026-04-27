package eventbus

import "sync"

// Registry is a thread-safe collection of named typed buses. Because Go
// generics cannot parameterize methods, typed access is provided by the
// package-level helper RegistryGet.
type Registry struct {
	mu    sync.Mutex
	buses map[string]any
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{buses: make(map[string]any)}
}

// RegistryGet returns the *Bus[T] registered under name, creating it lazily
// on first access. Subsequent calls with the same name and matching type T
// return the same instance. If a bus with the given name already exists with
// a different element type, RegistryGet panics; the name/type pairing is a
// program-level invariant.
func RegistryGet[T any](r *Registry, name string) *Bus[T] {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.buses[name]; ok {
		b, ok := existing.(*Bus[T])
		if !ok {
			panic("eventbus: registry type mismatch for bus " + name)
		}
		return b
	}
	b := New[T]()
	r.buses[name] = b
	return b
}

// Close closes every bus in the registry.
func (r *Registry) Close() {
	r.mu.Lock()
	buses := r.buses
	r.buses = make(map[string]any)
	r.mu.Unlock()
	for _, b := range buses {
		if c, ok := b.(interface{ Close() }); ok {
			c.Close()
		}
	}
}
