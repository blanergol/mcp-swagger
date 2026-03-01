package tool

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// TestEchoInputMessageMatrix protects message parsing contract for supported input shapes.
func TestEchoInputMessageMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      any
		want    string
		wantErr bool
	}{
		{name: "map any", in: map[string]any{"message": "hi"}, want: "hi"},
		{name: "map string", in: map[string]string{"message": "hello"}, want: "hello"},
		{name: "direct string", in: "echo", want: "echo"},
		{name: "missing key", in: map[string]any{"x": "y"}, wantErr: true},
		{name: "non string message", in: map[string]any{"message": 10}, wantErr: true},
		{name: "empty message map any", in: map[string]any{"message": ""}, wantErr: true},
		{name: "empty direct string", in: "", wantErr: true},
		{name: "unsupported type", in: []string{"x"}, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := inputMessage(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected inputMessage error")
				}
				return
			}
			if err != nil {
				t.Fatalf("inputMessage unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("inputMessage=%q want %q", got, tc.want)
			}
		})
	}
}

// TestEchoAndHealthExecuteContracts protects primary tool execution contracts.
func TestEchoAndHealthExecuteContracts(t *testing.T) {
	t.Parallel()

	echo := NewEchoTool()
	out, err := echo.Execute(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("echo execute error: %v", err)
	}
	outMap, ok := out.(map[string]any)
	if !ok || outMap["message"] != "hello" {
		t.Fatalf("unexpected echo output: %#v", out)
	}

	health := NewHealthTool("1.2.3")
	healthOut, err := health.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("health execute error: %v", err)
	}
	healthMap, ok := healthOut.(map[string]any)
	if !ok {
		t.Fatalf("unexpected health output type: %T", healthOut)
	}
	if healthMap["status"] != "ok" || healthMap["version"] != "1.2.3" {
		t.Fatalf("unexpected health output: %#v", healthMap)
	}
}

// TestRegistryMatrix protects register/list/get behavior and sorted order.
func TestRegistryMatrix(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(nil)
	echo := NewEchoTool()
	health := NewHealthTool("v1")
	r.Register(echo)
	r.Register(health)
	r.Register(NewHealthTool("v2")) // replace by name

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}
	names := []string{list[0].Name(), list[1].Name()}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("tool list must be sorted, got %v", names)
	}

	gotEcho, ok := r.Get("echo")
	if !ok || gotEcho == nil || gotEcho.Name() != "echo" {
		t.Fatalf("expected to get echo tool")
	}
	gotHealth, ok := r.Get("health")
	if !ok || gotHealth == nil {
		t.Fatalf("expected to get health tool")
	}
	res, err := gotHealth.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("health execute from registry error: %v", err)
	}
	if res.(map[string]any)["version"] != "v2" {
		t.Fatalf("expected replaced health tool version v2, got %#v", res)
	}

	missing, ok := r.Get("missing")
	if ok || missing != nil {
		t.Fatalf("expected missing tool lookup to return nil,false")
	}
}

// TestRegistryDeterminismProperty protects determinism invariant for repeated List calls.
func TestRegistryDeterminismProperty(t *testing.T) {
	t.Parallel()

	r := NewRegistry(NewEchoTool(), NewHealthTool("v1"))
	a := r.List()
	b := r.List()
	if !toolNamesEqual(a, b) {
		t.Fatalf("registry list must be deterministic")
	}

	// Additional randomized registration order should still list deterministically.
	randSrc := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		r2 := NewRegistry()
		tools := []Tool{NewEchoTool(), NewHealthTool("v1")}
		randSrc.Shuffle(len(tools), func(i, j int) { tools[i], tools[j] = tools[j], tools[i] })
		for _, tool := range tools {
			r2.Register(tool)
		}
		list := r2.List()
		names := []string{list[0].Name(), list[1].Name()}
		if !sort.StringsAreSorted(names) {
			t.Fatalf("registry list must be sorted, got %v", names)
		}
	}
}

// TestRegistryConcurrentAccess protects thread-safe behavior under concurrent register/list/get.
func TestRegistryConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				r.Register(NewEchoTool())
			} else {
				r.Register(NewHealthTool("v"))
			}
			list := r.List()
			for _, tool := range list {
				if _, err := tool.Execute(ctx, map[string]any{"message": "x"}); err != nil && !errors.Is(err, context.Canceled) {
					if tool.Name() == "health" {
						continue
					}
					t.Errorf("execute %s error: %v", tool.Name(), err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func toolNamesEqual(a, b []Tool) bool {
	if len(a) != len(b) {
		return false
	}
	left := make([]string, len(a))
	right := make([]string, len(b))
	for i := range a {
		left[i] = a[i].Name()
		right[i] = b[i].Name()
	}
	return reflect.DeepEqual(left, right)
}
