package swagger

import (
	"strings"
	"testing"
)

// FuzzParseRawObjectNoPanic protects parser robustness against malformed JSON/YAML fragments.
func FuzzParseRawObjectNoPanic(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"{}",
		"[]",
		"null",
		"true",
		"123",
		"\"text\"",
		minimalOpenAPIJSON,
		minimalOpenAPIYAML,
		"openapi: 3.0.3\ninfo:\n  title: x\n  version: y\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      responses:\n        '200':\n          description: ok\n",
		"openapi: [",
		"{\"openapi\":",
		"{invalid-json}",
		"x: [1,2,3",
		"x:\n  - y\n    : z",
		"---\nopenapi: 3.0.3\n",
		"a: b\nc: d\n",
		"{\"a\":{\"b\":[1,2,3]}}",
		"{\"$ref\":\"#/components/schemas/Pet\"}",
		"openapi: 3.0.3\ninfo:\n  title: 你好\n  version: \"1\"\npaths: {}",
		"openapi: 3.0.3\ninfo:\n  title: with-tab\n\tversion: '1'",
		"\n\n\n",
		"# comment only",
		"{\"arr\":[{\"x\":1},{\"y\":2}]}\n",
		"x: !!binary aGVsbG8=",
		"x: !!timestamp 2026-03-01T00:00:00Z",
		"{\"deep\":{\"a\":{\"b\":{\"c\":{\"d\":1}}}}}",
		"x: {a: 1, b: [2,3], c: {d: 4}}",
		"[\"a\",\"b\",\"c\"]",
		"---\n- a\n- b\n- c\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, raw string) {
		for _, format := range []string{"json", "yaml", "auto"} {
			p := NewOpenAPIParser(format)
			_, _ = p.parseRawObject([]byte(raw))
		}
	})
}

// FuzzSourceDetectionNoPanic protects URL/file source detection from panics on odd input.
func FuzzSourceDetectionNoPanic(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"./openapi.yaml",
		"../openapi.json",
		"C:/work/spec/openapi.yaml",
		"C:\\work\\spec\\openapi.yaml",
		"/etc/openapi.yaml",
		"http://example.com/openapi.yaml",
		"https://example.com/openapi.json",
		"HTTPS://EXAMPLE.COM/openapi.json",
		"file:///tmp/openapi.yaml",
		"ftp://example.com/openapi.yaml",
		"mailto:user@example.com",
		"javascript:alert(1)",
		"data:application/json,{}",
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
			t.Fatalf("parseURLSource returned URL without scheme for %q", raw)
		}

		kind, _, err := detectSourceKind(raw)
		scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
		supported := scheme == "http" || scheme == "https"
		if supported {
			if err != nil {
				t.Fatalf("detectSourceKind unexpected error for %q: %v", raw, err)
			}
			if kind != sourceKindHTTP {
				t.Fatalf("expected sourceKindHTTP for %q, got %q", raw, kind)
			}
		} else {
			if err == nil {
				t.Fatalf("expected unsupported-scheme error for %q", raw)
			}
		}
	})
}
