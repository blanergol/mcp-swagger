package prompt

import "testing"

// TestMemoryStoreDefaultsAndSortedList protects default prompts and deterministic listing order.
func TestMemoryStoreDefaultsAndSortedList(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	got, err := store.Get("welcome")
	if err != nil {
		t.Fatalf("expected default welcome prompt, got error: %v", err)
	}
	if got == "" {
		t.Fatalf("expected non-empty welcome template")
	}

	list := store.List()
	if len(list) < 2 {
		t.Fatalf("expected at least two default prompts, got %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Name > list[i].Name {
			t.Fatalf("list must be sorted by name: %q before %q", list[i-1].Name, list[i].Name)
		}
	}
}

// TestMemoryStoreListReturnsDefensiveCopy protects against accidental mutation via returned descriptors.
func TestMemoryStoreListReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(Item{
		Descriptor: Descriptor{Name: "demo", Arguments: []string{"a", "b"}},
		Template:   "value={{.a}}",
	})

	first := store.List()
	if len(first) != 1 {
		t.Fatalf("expected one descriptor, got %d", len(first))
	}
	first[0].Arguments[0] = "mutated"

	second := store.List()
	if second[0].Arguments[0] != "a" {
		t.Fatalf("store must return defensive copy of arguments, got %#v", second[0].Arguments)
	}
}

// TestMemoryStoreAddAndGetNotFound protects add/get contract and not-found behavior.
func TestMemoryStoreAddAndGetNotFound(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	store.Add(Item{
		Descriptor: Descriptor{Name: "custom"},
		Template:   "custom-template",
	})

	got, err := store.Get("custom")
	if err != nil {
		t.Fatalf("expected custom prompt, got error: %v", err)
	}
	if got != "custom-template" {
		t.Fatalf("unexpected template: %q", got)
	}

	_, err = store.Get("missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
