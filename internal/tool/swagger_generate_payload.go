package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

const (
	// payloadStrategyMinimal определяет стратегию выбора/генерации данных в соответствующем алгоритме.
	payloadStrategyMinimal = "minimal"
	// payloadStrategyExample определяет стратегию выбора/генерации данных в соответствующем алгоритме.
	payloadStrategyExample = "example"
	// payloadStrategyMaximal определяет стратегию выбора/генерации данных в соответствующем алгоритме.
	payloadStrategyMaximal = "maximal"
)

// SwaggerGeneratePayloadTool генерирует `params.body` по request-body схеме операции.
type SwaggerGeneratePayloadTool struct {
	store swagger.Store
}

// NewSwaggerGeneratePayloadTool создает swagger.http.generate_payload tool.
func NewSwaggerGeneratePayloadTool(store swagger.Store) *SwaggerGeneratePayloadTool {
	return &SwaggerGeneratePayloadTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerGeneratePayloadTool) Name() string {
	return ToolSwaggerHTTPGeneratePayload
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerGeneratePayloadTool) Description() string {
	return "Generates params.body from operation request schema (minimal|example|maximal)"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerGeneratePayloadTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerGeneratePayloadTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerGeneratePayloadTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}

	parsed, err := parseGeneratePayloadInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	endpoint, err := t.store.GetEndpointByOperationID(ctx, parsed.OperationID)
	if err != nil {
		return errorResult("invalid_request", "operationId is not found in swagger", map[string]any{"operationId": parsed.OperationID}), nil
	}

	if endpoint.Request.BodySchema == nil && endpoint.Request.Examples == nil {
		contentTypes := append([]string(nil), endpoint.Request.ContentTypes...)
		if contentTypes == nil {
			contentTypes = []string{}
		}
		return okResult(map[string]any{
			"operationId":  parsed.OperationID,
			"strategy":     parsed.Strategy,
			"seed":         parsed.Seed,
			"contentTypes": contentTypes,
			"body":         nil,
			"warnings":     []string{"operation does not define request body schema; returning null body"},
		}), nil
	}

	generator := newPayloadGenerator(parsed.Strategy, parsed.Seed)
	body := generator.generate(endpoint.Request.BodySchema, "$", 0)

	if parsed.Strategy == payloadStrategyExample && endpoint.Request.Examples != nil {
		if example, ok := pickRequestExample(endpoint.Request.Examples); ok {
			body = cloneJSONValue(example)
			generator.warn("used requestBody example from OpenAPI media examples")
		}
	}

	if parsed.Overrides != nil {
		body = mergePayloadOverrides(body, parsed.Overrides, generator)
	}

	return okResult(map[string]any{
		"operationId":  parsed.OperationID,
		"strategy":     parsed.Strategy,
		"seed":         parsed.Seed,
		"contentTypes": append([]string(nil), endpoint.Request.ContentTypes...),
		"body":         body,
		"warnings":     uniqueStrings(generator.warnings),
	}), nil
}

// generatePayloadInput хранит промежуточные данные инструмента между этапами подготовки и валидации.
type generatePayloadInput struct {
	OperationID string
	Seed        int64
	Strategy    string
	Overrides   map[string]any
}

// parseGeneratePayloadInput разбирает входные данные и возвращает нормализованное представление.
func parseGeneratePayloadInput(input any) (generatePayloadInput, error) {
	envelope, err := parseToolInputEnvelope(input, true)
	if err != nil {
		return generatePayloadInput{}, err
	}
	inMap, err := toAnyMap(input)
	if err != nil {
		return generatePayloadInput{}, err
	}

	query := envelope.Params.Query
	if query == nil {
		query, _ = toAnyMapOrNil(inMap["query"])
	}

	strategy := strings.ToLower(strings.TrimSpace(valueAsString(firstNonNil(
		mapValue(query, "strategy"),
		inMap["strategy"],
	))))
	if strategy == "" {
		strategy = payloadStrategyMinimal
	}
	switch strategy {
	case payloadStrategyMinimal, payloadStrategyExample, payloadStrategyMaximal:
	default:
		return generatePayloadInput{}, fmt.Errorf("strategy must be one of %q|%q|%q", payloadStrategyMinimal, payloadStrategyExample, payloadStrategyMaximal)
	}

	seed := int64(1)
	if raw := firstNonNil(mapValue(query, "seed"), inMap["seed"]); raw != nil {
		parsed, parseErr := parseInt64Any(raw)
		if parseErr != nil {
			return generatePayloadInput{}, fmt.Errorf("seed must be an integer: %w", parseErr)
		}
		seed = parsed
	}

	overridesRaw := firstNonNil(
		mapValue(query, "overrides"),
		inMap["overrides"],
	)
	overrides, err := toAnyMapOrNil(overridesRaw)
	if err != nil {
		return generatePayloadInput{}, fmt.Errorf("overrides must be an object: %w", err)
	}

	return generatePayloadInput{
		OperationID: strings.TrimSpace(envelope.OperationID),
		Seed:        seed,
		Strategy:    strategy,
		Overrides:   overrides,
	}, nil
}

// parseInt64Any разбирает входные данные и возвращает нормализованное представление.
func parseInt64Any(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

// payloadGenerator хранит промежуточные данные инструмента между этапами подготовки и валидации.
type payloadGenerator struct {
	strategy string
	random   *rand.Rand
	warnings []string
}

// newPayloadGenerator инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newPayloadGenerator(strategy string, seed int64) *payloadGenerator {
	if seed == 0 {
		seed = 1
	}
	src := rand.NewSource(seed) //nolint:gosec // deterministic test-friendly pseudo-randomness
	return &payloadGenerator{
		strategy: strategy,
		random:   rand.New(src),
	}
}

// warn добавляет предупреждение в результат генерации без прерывания основного потока.
func (g *payloadGenerator) warn(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	g.warnings = append(g.warnings, message)
}

// generate генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generate(schema any, path string, depth int) any {
	if depth > 32 {
		g.warn("max schema nesting depth reached at " + path)
		return nil
	}
	schemaMap, ok := schemaAsMap(schema)
	if !ok || len(schemaMap) == 0 {
		return nil
	}

	if example, ok := firstSchemaExample(schemaMap); ok {
		return cloneJSONValue(example)
	}

	if enumValues, ok := schemaEnum(schemaMap); ok && len(enumValues) > 0 {
		return cloneJSONValue(enumValues[0])
	}

	if options, ok := schemaArray(schemaMap["oneOf"]); ok && len(options) > 0 {
		idx := chooseCompositeOption(g.strategy, len(options))
		g.warn(fmt.Sprintf("oneOf at %s: selected option %d, other options are conditional", path, idx))
		return g.generate(options[idx], path+"/oneOf/"+strconv.Itoa(idx), depth+1)
	}
	if options, ok := schemaArray(schemaMap["anyOf"]); ok && len(options) > 0 {
		idx := chooseCompositeOption(g.strategy, len(options))
		g.warn(fmt.Sprintf("anyOf at %s: selected option %d, other options are conditional", path, idx))
		return g.generate(options[idx], path+"/anyOf/"+strconv.Itoa(idx), depth+1)
	}
	if options, ok := schemaArray(schemaMap["allOf"]); ok && len(options) > 0 {
		return g.generateAllOf(options, path, depth+1)
	}

	switch schemaTypeOrInfer(schemaMap) {
	case "object":
		return g.generateObject(schemaMap, path, depth+1)
	case "array":
		return g.generateArray(schemaMap, path, depth+1)
	case "integer":
		return g.generateInteger(schemaMap)
	case "number":
		return g.generateNumber(schemaMap)
	case "boolean":
		return true
	case "null":
		return nil
	case "string":
		fallthrough
	default:
		return g.generateString(schemaMap)
	}
}

// generateAllOf генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateAllOf(options []any, path string, depth int) any {
	merged := map[string]any{}
	mergedCount := 0
	for idx, option := range options {
		val := g.generate(option, path+"/allOf/"+strconv.Itoa(idx), depth+1)
		part, ok := val.(map[string]any)
		if !ok {
			g.warn(fmt.Sprintf("allOf at %s contains non-object option %d", path, idx))
			continue
		}
		merged = deepMergeMaps(merged, part)
		mergedCount++
	}
	if mergedCount == 0 {
		return nil
	}
	return merged
}

// generateObject генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateObject(schema map[string]any, path string, depth int) map[string]any {
	props, _ := schemaAsMap(schema["properties"])
	out := make(map[string]any)
	if len(props) == 0 {
		return out
	}

	required := make(map[string]struct{})
	for _, key := range schemaRequired(schema) {
		required[key] = struct{}{}
	}

	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	includeAll := g.strategy == payloadStrategyMaximal
	for _, key := range keys {
		_, isRequired := required[key]
		if !includeAll && !isRequired {
			continue
		}
		value := g.generate(props[key], path+"/"+key, depth+1)
		out[key] = value
	}

	for key := range required {
		if _, ok := out[key]; ok {
			continue
		}
		g.warn(fmt.Sprintf("required property %q at %s is not defined in properties", key, path))
		out[key] = "value"
	}
	return out
}

// generateArray генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateArray(schema map[string]any, path string, depth int) []any {
	items := schema["items"]
	count := 1
	if minItems, ok := numberAsInt(schema["minItems"]); ok && minItems > count {
		count = minItems
	}
	if g.strategy == payloadStrategyMaximal {
		count = 3
		if maxItems, ok := numberAsInt(schema["maxItems"]); ok && maxItems > 0 {
			count = maxItems
		}
		if count > 8 {
			count = 8
		}
		if minItems, ok := numberAsInt(schema["minItems"]); ok && count < minItems {
			count = minItems
		}
	}
	if count <= 0 {
		count = 1
	}

	out := make([]any, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, g.generate(items, path+"/items/"+strconv.Itoa(i), depth+1))
	}
	return out
}

// generateString генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateString(schema map[string]any) string {
	format := strings.ToLower(strings.TrimSpace(valueAsString(schema["format"])))
	minLength, _ := numberAsInt(schema["minLength"])
	maxLength, hasMaxLength := numberAsInt(schema["maxLength"])

	value := ""
	switch format {
	case "email":
		value = "user@example.com"
	case "uuid":
		value = fmt.Sprintf("%08x-%04x-4000-8000-%012x", g.random.Uint32(), g.random.Uint32()&0xffff, g.random.Uint64()&0xffffffffffff)
	case "date":
		value = "2025-01-01"
	case "date-time":
		value = "2025-01-01T00:00:00Z"
	case "uri", "url":
		value = "https://example.com/resource"
	case "ipv4":
		value = "203.0.113.10"
	case "ipv6":
		value = "2001:db8::1"
	default:
		value = fmt.Sprintf("value-%d", g.random.Intn(100000))
	}

	if minLength > 0 && len(value) < minLength {
		value = value + strings.Repeat("x", minLength-len(value))
	}
	if hasMaxLength && maxLength > 0 && len(value) > maxLength {
		value = value[:maxLength]
	}
	if pattern := strings.TrimSpace(valueAsString(schema["pattern"])); pattern != "" {
		g.warn("pattern constraint is not fully synthesized; generated fallback string")
	}
	return value
}

// generateInteger генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateInteger(schema map[string]any) int64 {
	if g.strategy == payloadStrategyMaximal {
		if maxValue, ok := numberAsFloat(schema["maximum"]); ok {
			return int64(math.Floor(maxValue))
		}
	}
	if minValue, ok := numberAsFloat(schema["minimum"]); ok {
		return int64(math.Ceil(minValue))
	}
	return int64(g.random.Intn(100))
}

// generateNumber генерирует значение по схеме с учетом выбранной стратегии и ограничений типов.
func (g *payloadGenerator) generateNumber(schema map[string]any) float64 {
	if g.strategy == payloadStrategyMaximal {
		if maxValue, ok := numberAsFloat(schema["maximum"]); ok {
			return maxValue
		}
	}
	if minValue, ok := numberAsFloat(schema["minimum"]); ok {
		return minValue
	}
	return float64(g.random.Intn(100)) + 0.5
}

// chooseCompositeOption выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func chooseCompositeOption(strategy string, count int) int {
	if count <= 1 {
		return 0
	}
	if strategy == payloadStrategyMaximal {
		return count - 1
	}
	return 0
}

// schemaAsMap выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaAsMap(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case nil:
		return nil, false
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		parsed := map[string]any{}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	}
}

// schemaArray выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaArray(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case nil:
		return nil, false
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var out []any
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}

// firstSchemaExample возвращает первое подходящее значение по приоритетному правилу выбора.
func firstSchemaExample(schema map[string]any) (any, bool) {
	if schema == nil {
		return nil, false
	}
	if example, ok := schema["example"]; ok && example != nil {
		return cloneJSONValue(example), true
	}
	if rawExamples, ok := schema["examples"]; ok {
		if arr, ok := schemaArray(rawExamples); ok {
			for _, item := range arr {
				if item != nil {
					return cloneJSONValue(item), true
				}
			}
		}
		if m, ok := schemaAsMap(rawExamples); ok && len(m) > 0 {
			keys := make([]string, 0, len(m))
			for key := range m {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if m[key] != nil {
					return cloneJSONValue(m[key]), true
				}
			}
		}
	}
	return nil, false
}

// pickRequestExample выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func pickRequestExample(value any) (any, bool) {
	if value == nil {
		return nil, false
	}
	if arr, ok := schemaArray(value); ok {
		for _, item := range arr {
			if item != nil {
				return cloneJSONValue(item), true
			}
		}
	}
	if m, ok := schemaAsMap(value); ok {
		// Если examples представлены как map[name]example, берем первый ключ в отсортированном порядке.
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return nil, false
		}

		// Эвристика: если значение по первому ключу похоже на полноценный объект примера, используем его.
		firstKey := keys[0]
		firstValue := m[firstKey]
		if _, isMap := firstValue.(map[string]any); isMap {
			return cloneJSONValue(firstValue), true
		}
		// Иначе трактуем исходное значение как уже готовый example-объект.
		return cloneJSONValue(m), true
	}
	return cloneJSONValue(value), true
}

// schemaEnum выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaEnum(schema map[string]any) ([]any, bool) {
	raw, ok := schema["enum"]
	if !ok || raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case []any:
		return v, len(v) > 0
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

// schemaTypeOrInfer выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaTypeOrInfer(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	if t := schemaType(schema); t != "" {
		return t
	}
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	return ""
}

// numberAsFloat выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func numberAsFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// numberAsInt выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func numberAsInt(value any) (int, bool) {
	f, ok := numberAsFloat(value)
	if !ok {
		return 0, false
	}
	return int(math.Round(f)), true
}

// mergePayloadOverrides выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mergePayloadOverrides(generated any, overrides map[string]any, g *payloadGenerator) any {
	if len(overrides) == 0 {
		return generated
	}
	baseMap, ok := generated.(map[string]any)
	if !ok {
		if g != nil {
			g.warn("generated body is not object; overrides replaced body entirely")
		}
		return cloneJSONValue(overrides)
	}
	return deepMergeMaps(baseMap, overrides)
}

// deepMergeMaps выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func deepMergeMaps(base map[string]any, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	out := cloneJSONValue(base).(map[string]any)
	for key, patchValue := range patch {
		currentValue, exists := out[key]
		patchMap, patchIsMap := patchValue.(map[string]any)
		currentMap, currentIsMap := currentValue.(map[string]any)
		if exists && patchIsMap && currentIsMap {
			out[key] = deepMergeMaps(currentMap, patchMap)
			continue
		}
		out[key] = cloneJSONValue(patchValue)
	}
	return out
}

// cloneJSONValue создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneJSONValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}
