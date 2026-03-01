package resource

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// TestMemoryStoreGetByIDAndURI protects retrieval contract by descriptor ID and URI.
func TestMemoryStoreGetByIDAndURI(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	byID, err := store.Get(context.Background(), "status")
	if err != nil {
		t.Fatalf("expected item by ID, got error: %v", err)
	}
	if byID.Text != "ok" {
		t.Fatalf("unexpected item text by ID: %q", byID.Text)
	}

	byURI, err := store.Get(context.Background(), "resource://status")
	if err != nil {
		t.Fatalf("expected item by URI, got error: %v", err)
	}
	if byURI.Text != "ok" {
		t.Fatalf("unexpected item text by URI: %q", byURI.Text)
	}
}

// TestMemoryStoreAddListAndReplaceMatrix protects add/list/replace and fallback-ID behavior.
func TestMemoryStoreAddListAndReplaceMatrix(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(
		Item{Descriptor: Descriptor{ID: "b", URI: "resource://b"}, Text: "b1"},
		Item{Descriptor: Descriptor{ID: "a", URI: "resource://a"}, Text: "a1"},
	)

	store.Add(Item{Descriptor: Descriptor{URI: "resource://c"}, Text: "c1"}) // fallback ID == URI
	store.Add(Item{Descriptor: Descriptor{ID: "a", URI: "resource://a"}, Text: "a2"})

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 descriptors, got %d", len(list))
	}

	ids := []string{list[0].ID, list[1].ID, list[2].ID}
	wantIDs := []string{"a", "b", "resource://c"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("list must be sorted by ID, got %v want %v", ids, wantIDs)
	}

	replaced, err := store.Get(context.Background(), "a")
	if err != nil {
		t.Fatalf("expected replaced item, got error: %v", err)
	}
	if replaced.Text != "a2" {
		t.Fatalf("expected replaced value a2, got %q", replaced.Text)
	}

	fallback, err := store.Get(context.Background(), "resource://c")
	if err != nil {
		t.Fatalf("expected fallback-id item by URI, got error: %v", err)
	}
	if fallback.Descriptor.ID != "resource://c" {
		t.Fatalf("expected fallback id to be uri, got %q", fallback.Descriptor.ID)
	}

	_, err = store.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing item, got %v", err)
	}
}

// TestMemoryStoreListDeterminismProperty protects deterministic list ordering across shuffled insert order.
func TestMemoryStoreListDeterminismProperty(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		count := 1 + r.Intn(20)
		items := make([]Item, 0, count)
		for j := 0; j < count; j++ {
			id := fmt.Sprintf("id-%02d", j)
			items = append(items, Item{Descriptor: Descriptor{ID: id, URI: "resource://" + id}, Text: id})
		}
		r.Shuffle(len(items), func(a, b int) { items[a], items[b] = items[b], items[a] })

		store := NewMemoryStore(items...)
		list1, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("list1 error: %v", err)
		}
		list2, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("list2 error: %v", err)
		}
		if !reflect.DeepEqual(list1, list2) {
			t.Fatalf("list must be deterministic between calls")
		}

		ids := make([]string, len(list1))
		for i := range list1 {
			ids[i] = list1[i].ID
		}
		if !sort.StringsAreSorted(ids) {
			t.Fatalf("list ids must be sorted: %v", ids)
		}
	}
}

// TestMemoryStoreConcurrentAddAndGet protects thread-safe behavior under concurrent writes/reads.
func TestMemoryStoreConcurrentAddAndGet(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	ctx := context.Background()

	const workers = 8
	const perWorker = 30
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				store.Add(Item{Descriptor: Descriptor{ID: id, URI: "resource://" + id}, Text: id})
				if _, err := store.Get(ctx, id); err != nil {
					t.Errorf("get after add failed for %s: %v", id, err)
				}
			}
		}()
	}
	wg.Wait()

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) < 1+workers*perWorker {
		t.Fatalf("unexpected descriptor count after concurrent adds: %d", len(list))
	}
}
