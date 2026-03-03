package swagger

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

var extraSiblingFieldsPattern = regexp.MustCompile(`extra sibling fields:\s*\[([^\]]+)\]`)

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

	spec, convertedFromSwaggerV2, err := p.loadOpenAPISpec(ctx, raw, parsedRaw)
	if err != nil {
		return nil, err
	}
	if err := validateOpenAPIDocument(ctx, spec); err != nil && !convertedFromSwaggerV2 {
		return nil, fmt.Errorf("validate openapi document: %w", err)
	}

	return &Document{Spec: spec, Raw: parsedRaw}, nil
}

// loadOpenAPISpec парсит OpenAPI v3 или конвертирует Swagger v2 в OpenAPI v3.
func (p *OpenAPIParser) loadOpenAPISpec(ctx context.Context, raw []byte, parsedRaw any) (*openapi3.T, bool, error) {
	if isSwaggerV2Document(parsedRaw) {
		spec, err := parseSwaggerV2Document(parsedRaw)
		if err != nil {
			return nil, false, fmt.Errorf("parse swagger v2 document: %w", err)
		}
		return spec, true, nil
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = ctx

	spec, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, false, fmt.Errorf("parse openapi document: %w", err)
	}
	return spec, false, nil
}

// validateOpenAPIDocument выполняет strict validate с fallback для распространенных несовместимостей.
func validateOpenAPIDocument(ctx context.Context, spec *openapi3.T) error {
	if spec == nil {
		return fmt.Errorf("empty parsed openapi document")
	}
	err := spec.Validate(ctx)
	if err == nil {
		return nil
	}

	strictErr := err
	options := []openapi3.ValidationOption{
		openapi3.DisableExamplesValidation(),
		openapi3.DisableSchemaDefaultsValidation(),
		openapi3.DisableSchemaPatternValidation(),
	}
	extraSiblingFields := extractExtraSiblingFields(strictErr.Error())
	if len(extraSiblingFields) > 0 {
		options = append(options, openapi3.AllowExtraSiblingFields(extraSiblingFields...))
	}
	if relaxedErr := spec.Validate(ctx, options...); relaxedErr == nil {
		return nil
	}
	return strictErr
}

// isSwaggerV2Document возвращает true, если root объект содержит swagger версии 2.x.
func isSwaggerV2Document(raw any) bool {
	root, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if openapiVersion, ok := root["openapi"]; ok {
		if strings.TrimSpace(fmt.Sprint(openapiVersion)) != "" {
			return false
		}
	}
	swaggerVersion, ok := root["swagger"]
	if !ok {
		return false
	}
	value := strings.TrimSpace(fmt.Sprint(swaggerVersion))
	return strings.HasPrefix(value, "2.")
}

// parseSwaggerV2Document конвертирует swagger 2.0 payload в openapi 3.x document.
func parseSwaggerV2Document(raw any) (*openapi3.T, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var specV2 openapi2.T
	if err := json.Unmarshal(payload, &specV2); err != nil {
		return nil, err
	}
	return openapi2conv.ToV3(&specV2)
}

// extractExtraSiblingFields достает имена полей из ошибок вида "extra sibling fields: [field]".
func extractExtraSiblingFields(errText string) []string {
	matches := extraSiblingFieldsPattern.FindAllStringSubmatch(errText, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	fields := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		parts := strings.FieldsFunc(match[1], func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for _, part := range parts {
			value := strings.TrimSpace(part)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			fields = append(fields, value)
		}
	}
	sort.Strings(fields)
	return fields
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
