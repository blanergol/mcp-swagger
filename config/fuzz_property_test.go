package config

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// TestNormalizeUpperListIdempotentProperty protects idempotency and dedup invariants.
func TestNormalizeUpperListIdempotentProperty(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	alphabet := []string{"", "get", "GET", "post", "Post", " put ", "patch", "delete", "options", "head", "trace", "connect", "\t", "\n"}

	for i := 0; i < 500; i++ {
		n := r.Intn(20)
		in := make([]string, n)
		for j := 0; j < n; j++ {
			in[j] = alphabet[r.Intn(len(alphabet))]
		}

		n1 := normalizeUpperList(in)
		n2 := normalizeUpperList(n1)
		if !reflect.DeepEqual(n1, n2) {
			t.Fatalf("normalizeUpperList must be idempotent: n1=%v n2=%v", n1, n2)
		}
		seen := map[string]struct{}{}
		for _, v := range n1 {
			if strings.TrimSpace(v) == "" || v != strings.ToUpper(v) {
				t.Fatalf("normalized value must be uppercase non-empty, got %q", v)
			}
			if _, ok := seen[v]; ok {
				t.Fatalf("normalized list must be unique, got %v", n1)
			}
			seen[v] = struct{}{}
		}
	}
}

// TestSplitListDeterministicProperty protects deterministic and stable-order behavior.
func TestSplitListDeterministicProperty(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"GET,POST,DELETE",
		" GET POST DELETE ",
		"a,a,b,b,c",
		"a\n b\t c",
		" a, b, c , a ",
		"пример,тест",
	}
	for _, in := range inputs {
		a := splitList(in)
		b := splitList(in)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("splitList must be deterministic for %q: %v vs %v", in, a, b)
		}
	}
}

// FuzzParseURLSourceNoPanic protects local-path vs URL detection from malformed values.
func FuzzParseURLSourceNoPanic(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"./openapi.yaml",
		"../openapi.json",
		"C:/work/openapi.yaml",
		"C:\\work\\openapi.yaml",
		"/etc/openapi.yaml",
		"http://example.com/openapi.yaml",
		"https://example.com/openapi.json",
		"HTTPS://EXAMPLE.COM/openapi.json",
		"file:///tmp/openapi.yaml",
		"ftp://example.com/openapi.yaml",
		"mailto:user@example.com",
		"data:application/json,{}",
		"javascript:alert(1)",
		"http://",
		"https:///missing-host",
		"https://example.com:443/openapi.yaml",
		"https://example.com/openapi.yaml?token=abc",
		"https://example.com/openapi.yaml#fragment",
		"https://[::1]/openapi.yaml",
		"http://127.0.0.1/openapi.yaml",
		"swagger://endpoints",
		"openapi.yaml\n",
		"\topenapi.yaml\t",
		"\\\\server\\share\\openapi.yaml",
		"//network/path/openapi.yaml",
		"https://пример.рф/openapi.yaml",
		"https://xn--e1afmkfd.xn--p1ai/openapi.yaml",
		"http://example..com/openapi.yaml",
		"http://-bad-host/openapi.yaml",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		parsed, isURL := parseURLSource(raw)
		if !isURL {
			if parsed != nil {
				t.Fatalf("parseURLSource returned URL when isURL=false for %q", raw)
			}
			return
		}
		if parsed == nil {
			t.Fatalf("parseURLSource returned nil URL when isURL=true for %q", raw)
		}
		if strings.TrimSpace(parsed.Scheme) == "" {
			t.Fatalf("parseURLSource URL must have non-empty scheme for %q", raw)
		}
	})
}
