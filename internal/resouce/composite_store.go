package resource

import (
	"context"
	"errors"
)

// CompositeStore объединяет несколько resource.Store в единый источник.
type CompositeStore struct {
	stores []Store
}

// NewCompositeStore создает composite store.
func NewCompositeStore(stores ...Store) *CompositeStore {
	filtered := make([]Store, 0, len(stores))
	for _, store := range stores {
		if store != nil {
			filtered = append(filtered, store)
		}
	}
	return &CompositeStore{stores: filtered}
}

// List объединяет дескрипторы всех дочерних хранилищ без дубликатов.
func (s *CompositeStore) List(ctx context.Context) ([]Descriptor, error) {
	if len(s.stores) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	out := make([]Descriptor, 0)
	for _, store := range s.stores {
		descriptors, err := store.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, descriptor := range descriptors {
			key := descriptor.URI
			if descriptor.URITemplate != "" {
				key = descriptor.URITemplate
			}
			if key == "" {
				key = descriptor.ID
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, descriptor)
		}
	}
	return out, nil
}

// Get возвращает первый найденный ресурс из дочерних хранилищ.
func (s *CompositeStore) Get(ctx context.Context, id string) (Item, error) {
	if len(s.stores) == 0 {
		return Item{}, ErrNotFound
	}
	for _, store := range s.stores {
		item, err := store.Get(ctx, id)
		if err == nil {
			return item, nil
		}
		if errors.Is(err, ErrNotFound) {
			continue
		}
		return Item{}, err
	}
	return Item{}, ErrNotFound
}
