package tool

import (
	"sort"
	"sync"
)

// MemoryRegistry реализует in-memory реестр инструментов.
type MemoryRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry создает реестр и сразу регистрирует переданные инструменты.
func NewRegistry(tools ...Tool) *MemoryRegistry {
	registry := &MemoryRegistry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		registry.Register(t)
	}
	return registry
}

// Register добавляет инструмент в реестр или заменяет существующий с тем же именем.
func (r *MemoryRegistry) Register(t Tool) {
	if t == nil {
		return
	}
	r.mu.Lock()
	r.tools[t.Name()] = t
	r.mu.Unlock()
}

// List возвращает инструменты, отсортированные по имени.
func (r *MemoryRegistry) List() []Tool {
	r.mu.RLock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Tool, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name])
	}
	r.mu.RUnlock()
	return out
}

// Get возвращает инструмент по имени.
func (r *MemoryRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	return t, ok
}
