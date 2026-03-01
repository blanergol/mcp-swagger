package tool

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// TestParseStringListAnyMatrix protects include-list parsing and dedup behavior.
func TestParseStringListAnyMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty string", in: "", want: nil},
		{name: "comma separated", in: "endpoints,usage", want: []string{"endpoints", "usage"}},
		{name: "space separated", in: "endpoints usage", want: []string{"endpoints", "usage"}},
		{name: "mixed separators", in: " endpoints, usage\nstatus ", want: []string{"endpoints", "usage", "status"}},
		{name: "dedup case insensitive", in: "Endpoints,endpoints,USAGE", want: []string{"endpoints", "usage"}},
		{name: "slice strings", in: []string{"A", "b", "a"}, want: []string{"a", "b"}},
		{name: "slice any", in: []any{"A", json.Number("2"), true}, want: []string{"a", "2", "true"}},
		{name: "json-like object array", in: []map[string]any{{"k": "v"}}, want: nil},
		{name: "numbers only", in: []any{1, 2, 2}, want: []string{"1", "2"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseStringListAny(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseStringListAny(%#v)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseStatusCodeMatrix protects status parser behavior for supported and unsupported types.
func TestParseStatusCodeMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      any
		want    int
		wantErr bool
	}{
		{name: "int", in: int(200), want: 200},
		{name: "int32", in: int32(201), want: 201},
		{name: "int64", in: int64(202), want: 202},
		{name: "float64", in: float64(203), want: 203},
		{name: "float32", in: float32(204), want: 204},
		{name: "json number int", in: json.Number("205"), want: 205},
		{name: "json number float invalid", in: json.Number("205.5"), wantErr: true},
		{name: "string trimmed", in: " 206 ", want: 206},
		{name: "string invalid", in: "abc", wantErr: true},
		{name: "bool unsupported", in: true, wantErr: true},
		{name: "nil unsupported", in: nil, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStatusCode(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected parseStatusCode error for %#v", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStatusCode unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseStatusCode(%#v)=%d want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestValueAsStringMapMatrix protects map conversion contracts and invalid value errors.
func TestValueAsStringMapMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      any
		want    map[string]string
		wantErr bool
	}{
		{name: "nil", in: nil, want: nil},
		{name: "map string", in: map[string]string{"A": "x", "B": " y "}, want: map[string]string{"A": "x", "B": "y"}},
		{name: "map any string-like", in: map[string]any{"A": "x", "B": 12, "C": true}, want: map[string]string{"A": "x", "B": "12", "C": "true"}},
		{name: "map any with nil", in: map[string]any{"A": nil}, want: map[string]string{"A": ""}},
		{name: "struct marshalable", in: struct {
			A string `json:"a"`
		}{A: "x"}, want: map[string]string{"a": "x"}},
		{name: "map any invalid nested", in: map[string]any{"A": map[string]any{"x": 1}}, wantErr: true},
		{name: "slice unsupported", in: []string{"x"}, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := valueAsStringMap(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected conversion error for %#v", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("valueAsStringMap unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("valueAsStringMap(%#v)=%#v want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestEncodeRequestBodyMatrix protects request-body encoding/content-type behavior.
func TestEncodeRequestBodyMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            any
		contentType     string
		wantContentType string
		wantErr         bool
		assert          func(t *testing.T, payload []byte, body any)
	}{
		{name: "nil body", body: nil, contentType: "application/json", wantContentType: "application/json", assert: func(t *testing.T, payload []byte, body any) {
			t.Helper()
			if payload != nil || body != nil {
				t.Fatalf("nil body must produce nil payload/body")
			}
		}},
		{name: "json string auto content type", body: "{\"a\":1}", wantContentType: "application/json", assert: func(t *testing.T, payload []byte, _ any) {
			t.Helper()
			if !strings.Contains(string(payload), "\"a\":1") {
				t.Fatalf("unexpected payload: %s", string(payload))
			}
		}},
		{name: "json string invalid for json content-type", body: "{", contentType: "application/json", wantErr: true},
		{name: "plain string default", body: "hello", wantContentType: "text/plain; charset=utf-8", assert: func(t *testing.T, payload []byte, body any) {
			t.Helper()
			if string(payload) != "hello" || body != "hello" {
				t.Fatalf("unexpected plain payload/body")
			}
		}},
		{name: "bytes default octet-stream", body: []byte{0x01, 0x02}, wantContentType: "application/octet-stream", assert: func(t *testing.T, payload []byte, _ any) {
			t.Helper()
			if len(payload) != 2 || payload[0] != 0x01 {
				t.Fatalf("unexpected binary payload")
			}
		}},
		{name: "bytes json decode", body: []byte(`{"x":1}`), contentType: "application/json", wantContentType: "application/json", assert: func(t *testing.T, payload []byte, _ any) {
			t.Helper()
			if !strings.Contains(string(payload), "\"x\":1") {
				t.Fatalf("unexpected payload: %s", string(payload))
			}
		}},
		{name: "object marshaled json", body: map[string]any{"x": 1}, wantContentType: "application/json", assert: func(t *testing.T, payload []byte, _ any) {
			t.Helper()
			if !strings.Contains(string(payload), "\"x\":1") {
				t.Fatalf("unexpected payload: %s", string(payload))
			}
		}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			payload, ct, body, err := encodeRequestBody(tc.body, tc.contentType)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected encodeRequestBody error")
				}
				return
			}
			if err != nil {
				t.Fatalf("encodeRequestBody unexpected error: %v", err)
			}
			if tc.wantContentType != "" && ct != tc.wantContentType {
				t.Fatalf("content-type=%q want %q", ct, tc.wantContentType)
			}
			if tc.assert != nil {
				tc.assert(t, payload, body)
			}
		})
	}
}

// TestNormalizeResponseBodyMatrix protects response-body envelope normalization contracts.
func TestNormalizeResponseBodyMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		contentType  string
		bodyEncoding string
		body         any
		wantEncoding string
	}{
		{name: "nil defaults text", body: nil, wantEncoding: "text"},
		{name: "json explicit", contentType: "application/json", bodyEncoding: "json", body: "{\"x\":1}", wantEncoding: "json"},
		{name: "json inferred from content-type", contentType: "application/json", body: "{\"x\":1}", wantEncoding: "json"},
		{name: "text explicit", bodyEncoding: "text", body: 123, wantEncoding: "text"},
		{name: "base64 explicit", bodyEncoding: "base64", body: []byte("ok"), wantEncoding: "base64"},
		{name: "bytes decoded", body: []byte(`{"ok":true}`), wantEncoding: "json"},
		{name: "object to json", body: map[string]any{"ok": true}, wantEncoding: "json"},
		{name: "string text", body: "hello", wantEncoding: "text"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			envelope := normalizeResponseBody(tc.contentType, tc.bodyEncoding, tc.body)
			if envelope.BodyEncoding != tc.wantEncoding {
				t.Fatalf("encoding=%q want %q", envelope.BodyEncoding, tc.wantEncoding)
			}
			if envelope.ContentType == "" {
				t.Fatalf("content-type must not be empty")
			}
		})
	}
}

// TestUniqueStringsIdempotentProperty protects idempotency+dedup+trim invariants for uniqueStrings.
func TestUniqueStringsIdempotentProperty(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	alphabet := []string{"", "a", "A", "b", " c ", "d", "a", "b", "x", "x", " ", "\t"}
	for i := 0; i < 500; i++ {
		n := r.Intn(20)
		in := make([]string, n)
		for j := 0; j < n; j++ {
			in[j] = alphabet[r.Intn(len(alphabet))]
		}
		u1 := uniqueStrings(in)
		u2 := uniqueStrings(u1)
		if !reflect.DeepEqual(u1, u2) {
			t.Fatalf("uniqueStrings must be idempotent: u1=%v u2=%v", u1, u2)
		}
		if len(u1) == 0 {
			continue
		}
		seen := map[string]struct{}{}
		for _, item := range u1 {
			if strings.TrimSpace(item) == "" {
				t.Fatalf("uniqueStrings must not contain empty items: %v", u1)
			}
			if _, ok := seen[item]; ok {
				t.Fatalf("uniqueStrings output must be unique: %v", u1)
			}
			seen[item] = struct{}{}
		}
	}
}

// TestFlattenHTTPHeadersDeterministic protects stable sorted output contract for header flattening.
func TestFlattenHTTPHeadersDeterministic(t *testing.T) {
	t.Parallel()

	headers := map[string][]string{
		"X-B": {"2"},
		"X-A": {"1", "11"},
		"X-C": {"3"},
	}

	flat1 := flattenHTTPHeaders(headers)
	flat2 := flattenHTTPHeaders(headers)
	if !reflect.DeepEqual(flat1, flat2) {
		t.Fatalf("flattenHTTPHeaders must be deterministic")
	}
	if flat1["X-A"] != "1,11" || flat1["X-B"] != "2" || flat1["X-C"] != "3" {
		t.Fatalf("unexpected flattened headers: %#v", flat1)
	}
}
