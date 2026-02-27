package prompt

import (
	"sort"
	"sync"
)

// Item хранит метаданные prompt и текст шаблона.
type Item struct {
	Descriptor Descriptor
	Template   string
}

// MemoryStore реализует in-memory хранилище prompt-шаблонов.
type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]Item
}

// NewMemoryStore создает хранилище с опциональными начальными данными.
func NewMemoryStore(items ...Item) *MemoryStore {
	if len(items) == 0 {
		items = []Item{
			{
				Descriptor: Descriptor{
					Name:        "welcome",
					Description: "Simple greeting prompt",
					Arguments:   []string{"name"},
				},
				Template: "Hello, {{.name}}!",
			},
			{
				Descriptor: Descriptor{
					Name:        "swagger.call_agent",
					Description: "Guides agent to safely plan and execute swagger API calls via MCP tools",
					Arguments:   []string{"goal", "operationId"},
				},
				Template: "Goal: {{.goal}}\nOperation: {{.operationId}}\n\nWorkflow:\n1) Use swagger.plan_call to identify/confirm operation.\n2) Use swagger.http.generate_payload if request body is needed.\n3) Use swagger.http.prepare_request with params.path/params.query/params.headers/params.body.\n4) Use swagger.http.validate_request before execute.\n5) Use swagger.http.execute (real call through MCP gateway).\n6) Use swagger.http.validate_response to detect contract drift.\n\nNever call upstream APIs directly outside MCP.",
			},
		}
	}

	store := &MemoryStore{items: make(map[string]Item, len(items))}
	for _, item := range items {
		store.items[item.Descriptor.Name] = item
	}
	return store
}

// Add добавляет шаблон prompt или заменяет существующий с тем же именем.
func (s *MemoryStore) Add(item Item) {
	s.mu.Lock()
	s.items[item.Descriptor.Name] = item
	s.mu.Unlock()
}

// Get возвращает текст шаблона по имени prompt.
func (s *MemoryStore) Get(name string) (string, error) {
	s.mu.RLock()
	item, ok := s.items[name]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	return item.Template, nil
}

// List возвращает список дескрипторов prompt-шаблонов.
func (s *MemoryStore) List() []Descriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Descriptor, 0, len(s.items))
	for _, item := range s.items {
		d := item.Descriptor
		d.Arguments = append([]string(nil), d.Arguments...)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
