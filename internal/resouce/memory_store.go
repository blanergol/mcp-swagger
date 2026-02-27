package resource

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore реализует in-memory хранилище ресурсов.
type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]Item
}

// NewMemoryStore создает хранилище с опциональным набором начальных ресурсов.
func NewMemoryStore(items ...Item) *MemoryStore {
	if len(items) == 0 {
		items = []Item{
			{
				Descriptor: Descriptor{
					ID:          "status",
					Name:        "status",
					Description: "Server status resource",
					URI:         "resource://status",
					MIMEType:    "text/plain",
				},
				Text: "ok",
			},
		}
	}
	store := &MemoryStore{items: make(map[string]Item, len(items))}
	for _, item := range items {
		id := item.Descriptor.ID
		if id == "" {
			id = item.Descriptor.URI
			item.Descriptor.ID = id
		}
		store.items[id] = item
	}
	return store
}

// Add добавляет ресурс или заменяет существующий с тем же ID.
func (s *MemoryStore) Add(item Item) {
	id := item.Descriptor.ID
	if id == "" {
		id = item.Descriptor.URI
		item.Descriptor.ID = id
	}
	s.mu.Lock()
	s.items[id] = item
	s.mu.Unlock()
}

// List возвращает дескрипторы всех ресурсов, отсортированные по ID.
func (s *MemoryStore) List(context.Context) ([]Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Descriptor, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item.Descriptor)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Get возвращает ресурс по его ID.
func (s *MemoryStore) Get(_ context.Context, id string) (Item, error) {
	s.mu.RLock()
	item, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return Item{}, ErrNotFound
	}
	return item, nil
}
