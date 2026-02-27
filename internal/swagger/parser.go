package swagger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// OpenAPIParser парсит OpenAPI-спецификацию в режимах json/yaml/auto.
type OpenAPIParser struct {
	format string
}

// NewOpenAPIParser создает парсер с подсказкой формата: auto|json|yaml.
func NewOpenAPIParser(format string) *OpenAPIParser {
	mode := strings.ToLower(strings.TrimSpace(format))
	if mode == "" {
		mode = "auto"
	}
	return &OpenAPIParser{format: mode}
}

// Parse разбирает сырой payload в OpenAPI-документ и универсальное дерево объектов.
func (p *OpenAPIParser) Parse(ctx context.Context, raw []byte) (*Document, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty swagger payload")
	}

	parsedRaw, err := p.parseRawObject(raw)
	if err != nil {
		return nil, err
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = ctx

	spec, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	if err := spec.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate openapi document: %w", err)
	}

	return &Document{Spec: spec, Raw: parsedRaw}, nil
}

// parseRawObject разбирает входные данные и возвращает нормализованное представление.
func (p *OpenAPIParser) parseRawObject(raw []byte) (any, error) {
	switch p.format {
	case "json":
		return parseJSON(raw)
	case "yaml", "yml":
		return parseYAML(raw)
	case "auto":
		parsed, err := parseJSON(raw)
		if err == nil {
			return parsed, nil
		}
		return parseYAML(raw)
	default:
		return nil, fmt.Errorf("unsupported swagger format %q", p.format)
	}
}

// parseJSON разбирает входные данные и возвращает нормализованное представление.
func parseJSON(raw []byte) (any, error) {
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return normalizeYAML(out), nil
}

// parseYAML разбирает входные данные и возвращает нормализованное представление.
func parseYAML(raw []byte) (any, error) {
	var out any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return normalizeYAML(out), nil
}

// normalizeYAML нормализует входные данные к канонической форме, используемой в модуле.
func normalizeYAML(value any) any {
	switch v := value.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for key, val := range v {
			m[key] = normalizeYAML(val)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(v))
		for key, val := range v {
			m[fmt.Sprint(key)] = normalizeYAML(val)
		}
		return m
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = normalizeYAML(v[i])
		}
		return out
	default:
		return value
	}
}
