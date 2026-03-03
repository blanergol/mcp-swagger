package swagger

import (
	"context"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

const minimalOpenAPIYAML = `openapi: 3.0.3
info:
  title: test
  version: "1.0.0"
paths: {}
`

const minimalOpenAPIJSON = `{"openapi":"3.0.3","info":{"title":"test","version":"1.0.0"},"paths":{}}`
const minimalSwaggerV2JSON = `{"swagger":"2.0","info":{"title":"test","version":"1.0.0"},"host":"example.com","basePath":"/api","schemes":["https"],"paths":{"/ping":{"get":{"operationId":"getPing","responses":{"200":{"description":"ok"}}}}}}`
const openAPIWithExtraRefSiblingYAML = `openapi: 3.0.3
info:
  title: weather-like
  version: "1.0.0"
paths:
  /observations:
    get:
      operationId: getObservations
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ObservationCollectionGeoJson"
components:
  schemas:
    ObservationCollectionGeoJson:
      $ref: "#/components/schemas/FeatureCollectionGeoJson"
      pagination:
        type: object
        properties:
          next:
            type: string
    FeatureCollectionGeoJson:
      type: object
      properties:
        type:
          type: string
`

// TestParseRawObjectMatrix protects parser format selection and malformed-input behavior.
func TestParseRawObjectMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		raw     string
		wantErr bool
	}{
		{name: "json object", format: "json", raw: minimalOpenAPIJSON},
		{name: "json array", format: "json", raw: `[1,2,3]`},
		{name: "yaml object", format: "yaml", raw: minimalOpenAPIYAML},
		{name: "yaml alias", format: "yml", raw: minimalOpenAPIYAML},
		{name: "auto json", format: "auto", raw: minimalOpenAPIJSON},
		{name: "auto yaml", format: "auto", raw: minimalOpenAPIYAML},
		{name: "auto broken", format: "auto", raw: `{broken: [`, wantErr: true},
		{name: "json malformed", format: "json", raw: `{"x":`, wantErr: true},
		{name: "yaml malformed", format: "yaml", raw: "x:\n  - y\n    : z\n", wantErr: true},
		{name: "unsupported format", format: "toml", raw: `a=1`, wantErr: true},
		{name: "json scalar", format: "json", raw: `"hello"`},
		{name: "yaml scalar", format: "yaml", raw: `hello`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenAPIParser(tc.format)
			out, err := p.parseRawObject([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for format=%q raw=%q", tc.format, tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRawObject error: %v", err)
			}
			if out == nil {
				t.Fatalf("parseRawObject returned nil output")
			}
		})
	}
}

// TestOpenAPIParserParseMatrix protects document parse/validation contracts.
func TestOpenAPIParserParseMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		raw     []byte
		wantErr bool
		errText string
	}{
		{name: "valid yaml openapi", format: "yaml", raw: []byte(minimalOpenAPIYAML)},
		{name: "valid json openapi", format: "json", raw: []byte(minimalOpenAPIJSON)},
		{name: "valid auto yaml", format: "auto", raw: []byte(minimalOpenAPIYAML)},
		{name: "valid swagger v2 json", format: "auto", raw: []byte(minimalSwaggerV2JSON)},
		{name: "openapi with extra ref sibling field", format: "yaml", raw: []byte(openAPIWithExtraRefSiblingYAML)},
		{name: "empty payload", format: "auto", raw: nil, wantErr: true, errText: "empty swagger payload"},
		{name: "invalid json syntax", format: "json", raw: []byte(`{"openapi":`), wantErr: true},
		{name: "invalid yaml syntax", format: "yaml", raw: []byte("openapi: ["), wantErr: true},
		{name: "not openapi", format: "auto", raw: []byte(`{"x":1}`), wantErr: true},
		{name: "unsupported format", format: "xml", raw: []byte(minimalOpenAPIJSON), wantErr: true, errText: "unsupported swagger format"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenAPIParser(tc.format)
			doc, err := p.Parse(context.Background(), tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected parse error")
				}
				if tc.errText != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.errText)) {
					t.Fatalf("expected error to contain %q, got %v", tc.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if doc == nil || doc.Spec == nil || doc.Raw == nil {
				t.Fatalf("expected non-nil parsed document")
			}
		})
	}
}

// TestNormalizeYAMLIdempotentProperty protects normalizeYAML idempotency invariant.
func TestNormalizeYAMLIdempotentProperty(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 300; i++ {
		input := randomYAMLLikeValue(r, 0)
		n1 := normalizeYAML(input)
		n2 := normalizeYAML(n1)
		if !reflect.DeepEqual(n1, n2) {
			t.Fatalf("normalizeYAML must be idempotent: n1=%#v n2=%#v", n1, n2)
		}
	}
}

func randomYAMLLikeValue(r *rand.Rand, depth int) any {
	if depth >= 3 {
		return randomScalar(r)
	}
	switch r.Intn(5) {
	case 0:
		m := map[string]any{}
		n := r.Intn(4)
		for i := 0; i < n; i++ {
			m[randomString(r, 6)] = randomYAMLLikeValue(r, depth+1)
		}
		return m
	case 1:
		m := map[any]any{}
		n := r.Intn(4)
		for i := 0; i < n; i++ {
			m[randomScalar(r)] = randomYAMLLikeValue(r, depth+1)
		}
		return m
	case 2:
		arr := make([]any, r.Intn(5))
		for i := range arr {
			arr[i] = randomYAMLLikeValue(r, depth+1)
		}
		return arr
	default:
		return randomScalar(r)
	}
}

func randomScalar(r *rand.Rand) any {
	switch r.Intn(5) {
	case 0:
		return randomString(r, 8)
	case 1:
		return r.Intn(1000)
	case 2:
		return r.Float64()
	case 3:
		return r.Intn(2) == 0
	default:
		return nil
	}
}

func randomString(r *rand.Rand, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 1
	}
	n := 1 + r.Intn(maxLen)
	chars := []rune("abcdefghijklmnopqrstuvwxyz0123456789-_./")
	out := make([]rune, n)
	for i := range out {
		out[i] = chars[r.Intn(len(chars))]
	}
	return string(out)
}
