package coordinator

import "sync"

// ModuleRegistry keeps implementation wiring explicit and replaceable.
type ModuleRegistry struct {
	mu      sync.RWMutex
	modules map[string]any
}

func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{modules: map[string]any{}}
}

func (r *ModuleRegistry) Register(name string, module any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[name] = module
}

func (r *ModuleRegistry) Get(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[name]
	return m, ok
}
