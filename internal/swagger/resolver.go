package swagger

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// methodOrder хранит служебное значение, используемое внутри текущего пакета.
var methodOrder = map[string]int{
	"GET":     0,
	"POST":    1,
	"PUT":     2,
	"DELETE":  3,
	"PATCH":   4,
	"OPTIONS": 5,
	"HEAD":    6,
	"TRACE":   7,
}

// OpenAPIResolver разрешает endpoint details и schemas.
type OpenAPIResolver struct {
	baseURLOverride string
}

// NewOpenAPIResolver создает operation resolver.
func NewOpenAPIResolver(baseURLOverride string) *OpenAPIResolver {
	return &OpenAPIResolver{baseURLOverride: strings.TrimSpace(baseURLOverride)}
}

// ResolveEndpoint разрешает full endpoint payload.
func (r *OpenAPIResolver) ResolveEndpoint(_ context.Context, op Operation) (ResolvedOperation, error) {
	if op.Doc == nil || op.Doc.Spec == nil {
		return ResolvedOperation{}, fmt.Errorf("missing openapi document")
	}
	if op.Value == nil {
		return ResolvedOperation{}, fmt.Errorf("missing operation payload")
	}

	operationID := op.OperationID
	if operationID == "" {
		operationID = syntheticOperationID(op.Method, op.Path)
	}

	servers := r.resolveServers(op)
	baseURL := firstServer(servers)
	pathTemplate := op.Path
	resolved := ResolvedOperation{
		Method:       strings.ToUpper(op.Method),
		BaseURL:      baseURL,
		PathTemplate: pathTemplate,
		URLTemplate:  buildOperationURL(baseURL, pathTemplate),
		ExampleURL:   buildExampleOperationURL(baseURL, pathTemplate),
		OperationID:  operationID,
		Summary:      op.Value.Summary,
		Description:  op.Value.Description,
		Tags:         append([]string(nil), op.Value.Tags...),
		Deprecated:   op.Value.Deprecated,
		Servers:      servers,
		Security:     r.resolveSecurity(op),
		Request:      Request{},
		Responses:    ResponseGroups{},
	}

	params := r.mergeParameters(op)
	for _, param := range params {
		schema := r.resolveParameterSchema(param)
		value := Param{
			Name:        param.Name,
			In:          param.In,
			Required:    param.Required,
			Description: param.Description,
			Schema:      schema,
		}
		switch param.In {
		case openapi3.ParameterInPath:
			resolved.PathParams = append(resolved.PathParams, value)
		case openapi3.ParameterInQuery:
			resolved.QueryParams = append(resolved.QueryParams, value)
		case openapi3.ParameterInHeader:
			resolved.HeaderParams = append(resolved.HeaderParams, value)
		case openapi3.ParameterInCookie:
			resolved.CookieParams = append(resolved.CookieParams, value)
		}
	}

	sortParams(resolved.PathParams)
	sortParams(resolved.QueryParams)
	sortParams(resolved.HeaderParams)
	sortParams(resolved.CookieParams)

	resolved.Request = r.resolveRequest(op)
	resolved.Responses = r.resolveResponses(op)

	return resolved, nil
}

// resolveServers вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveServers(op Operation) []string {
	if r.baseURLOverride != "" {
		return []string{r.baseURLOverride}
	}
	var src openapi3.Servers
	switch {
	case op.Value != nil && op.Value.Servers != nil:
		src = *op.Value.Servers
	case op.PathItem != nil && len(op.PathItem.Servers) > 0:
		src = op.PathItem.Servers
	case op.Doc != nil && op.Doc.Spec != nil:
		src = op.Doc.Spec.Servers
	}
	out := make([]string, 0, len(src))
	for _, server := range src {
		if server == nil || strings.TrimSpace(server.URL) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(server.URL))
	}
	return dedupeStrings(out)
}

// resolveSecurity вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveSecurity(op Operation) any {
	if op.Value != nil && op.Value.Security != nil {
		return cloneViaJSON(*op.Value.Security)
	}
	if op.Doc != nil && op.Doc.Spec != nil {
		return cloneViaJSON(op.Doc.Spec.Security)
	}
	return nil
}

// mergeParameters выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (r *OpenAPIResolver) mergeParameters(op Operation) []*openapi3.Parameter {
	merged := make(map[string]*openapi3.Parameter)
	order := make([]string, 0)

	appendParam := func(ref *openapi3.ParameterRef) {
		if ref == nil || ref.Value == nil {
			return
		}
		p := ref.Value
		key := strings.ToLower(strings.TrimSpace(p.In)) + ":" + strings.TrimSpace(p.Name)
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		merged[key] = p
	}

	if op.PathItem != nil {
		for _, paramRef := range op.PathItem.Parameters {
			appendParam(paramRef)
		}
	}
	if op.Value != nil {
		for _, paramRef := range op.Value.Parameters {
			appendParam(paramRef)
		}
	}

	out := make([]*openapi3.Parameter, 0, len(order))
	for _, key := range order {
		if p := merged[key]; p != nil {
			out = append(out, p)
		}
	}
	return out
}

// resolveParameterSchema вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveParameterSchema(param *openapi3.Parameter) any {
	if param == nil {
		return nil
	}
	if param.Schema != nil {
		return r.resolveSchemaRef(param.Schema, make(map[*openapi3.Schema]struct{}))
	}
	if len(param.Content) == 0 {
		return nil
	}
	keys := sortedMediaTypeKeys(param.Content)
	for _, key := range keys {
		media := param.Content[key]
		if media == nil || media.Schema == nil {
			continue
		}
		return r.resolveSchemaRef(media.Schema, make(map[*openapi3.Schema]struct{}))
	}
	return nil
}

// resolveRequest вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveRequest(op Operation) Request {
	if op.Value == nil || op.Value.RequestBody == nil || op.Value.RequestBody.Value == nil {
		return Request{}
	}
	body := op.Value.RequestBody.Value
	contentTypes := sortedMediaTypeKeys(body.Content)
	media := preferredMedia(body.Content)

	request := Request{ContentTypes: contentTypes}
	if media != nil {
		request.BodySchema = r.resolveSchemaRef(media.Schema, make(map[*openapi3.Schema]struct{}))
		request.Examples = extractExamples(media)
	}
	return request
}

// resolveResponses вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveResponses(op Operation) ResponseGroups {
	groups := ResponseGroups{}
	if op.Value == nil || op.Value.Responses == nil {
		return groups
	}

	responseMap := op.Value.Responses.Map()
	keys := make([]string, 0, len(responseMap))
	for key := range responseMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return compareStatus(keys[i], keys[j]) < 0
	})

	for _, statusKey := range keys {
		responseRef := responseMap[statusKey]
		if responseRef == nil || responseRef.Value == nil {
			continue
		}
		response := responseRef.Value
		status := parseStatusCode(statusKey)
		entry := Response{
			Status:       status,
			Description:  stringPtrValue(response.Description),
			ContentTypes: sortedMediaTypeKeys(response.Content),
		}
		media := preferredMedia(response.Content)
		if media != nil {
			entry.BodySchema = r.resolveSchemaRef(media.Schema, make(map[*openapi3.Schema]struct{}))
		}

		switch {
		case status >= 200 && status < 400:
			groups.Success = append(groups.Success, entry)
		case status >= 400 && status < 600:
			groups.Errors = append(groups.Errors, ErrorResponse(entry))
		case status == 0 && strings.EqualFold(statusKey, "default"):
			groups.Errors = append(groups.Errors, ErrorResponse(entry))
		}
	}

	return groups
}

// resolveSchemaRef вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveSchemaRef(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]struct{}) any {
	if ref == nil {
		return nil
	}
	if ref.Value == nil {
		if ref.Ref == "" {
			return nil
		}
		return map[string]any{"x-unresolvedRef": ref.Ref}
	}
	return r.resolveSchema(ref.Value, ref.Ref, seen)
}

// resolveSchema вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveSchema(schema *openapi3.Schema, sourceRef string, seen map[*openapi3.Schema]struct{}) any {
	if schema == nil {
		return nil
	}
	if _, ok := seen[schema]; ok {
		if sourceRef != "" {
			return map[string]any{"x-circularRef": sourceRef}
		}
		return map[string]any{"x-circularRef": true}
	}
	seen[schema] = struct{}{}
	defer delete(seen, schema)

	out := make(map[string]any)
	if sourceRef != "" {
		out["x-originRef"] = sourceRef
	}

	if schema.Type != nil && len(*schema.Type) > 0 {
		if len(*schema.Type) == 1 {
			out["type"] = (*schema.Type)[0]
		} else {
			out["type"] = append([]string(nil), (*schema.Type)...)
		}
	}
	if schema.Title != "" {
		out["title"] = schema.Title
	}
	if schema.Format != "" {
		out["format"] = schema.Format
	}
	if schema.Description != "" {
		out["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		out["enum"] = cloneAny(schema.Enum)
	}
	if schema.Default != nil {
		out["default"] = cloneAny(schema.Default)
	}
	if schema.Example != nil {
		out["example"] = cloneAny(schema.Example)
	}
	if schema.Nullable {
		out["nullable"] = true
	}
	if schema.ReadOnly {
		out["readOnly"] = true
	}
	if schema.WriteOnly {
		out["writeOnly"] = true
	}
	if schema.Deprecated {
		out["deprecated"] = true
	}
	if schema.Min != nil {
		out["minimum"] = *schema.Min
	}
	if schema.Max != nil {
		out["maximum"] = *schema.Max
	}
	if schema.MultipleOf != nil {
		out["multipleOf"] = *schema.MultipleOf
	}
	if schema.MinLength > 0 {
		out["minLength"] = schema.MinLength
	}
	if schema.MaxLength != nil {
		out["maxLength"] = *schema.MaxLength
	}
	if schema.Pattern != "" {
		out["pattern"] = schema.Pattern
	}
	if schema.MinItems > 0 {
		out["minItems"] = schema.MinItems
	}
	if schema.MaxItems != nil {
		out["maxItems"] = *schema.MaxItems
	}
	if schema.UniqueItems {
		out["uniqueItems"] = true
	}
	if len(schema.Required) > 0 {
		required := append([]string(nil), schema.Required...)
		sort.Strings(required)
		out["required"] = required
	}
	if schema.MinProps > 0 {
		out["minProperties"] = schema.MinProps
	}
	if schema.MaxProps != nil {
		out["maxProperties"] = *schema.MaxProps
	}
	if schema.Items != nil {
		out["items"] = r.resolveSchemaRef(schema.Items, seen)
	}
	if schema.Not != nil {
		out["not"] = r.resolveSchemaRef(schema.Not, seen)
	}
	if len(schema.AllOf) > 0 {
		out["allOf"] = r.resolveSchemaRefs(schema.AllOf, seen)
	}
	if len(schema.AnyOf) > 0 {
		out["anyOf"] = r.resolveSchemaRefs(schema.AnyOf, seen)
	}
	if len(schema.OneOf) > 0 {
		out["oneOf"] = r.resolveSchemaRefs(schema.OneOf, seen)
	}
	if len(schema.Properties) > 0 {
		keys := make([]string, 0, len(schema.Properties))
		for key := range schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		props := make(map[string]any, len(keys))
		for _, key := range keys {
			props[key] = r.resolveSchemaRef(schema.Properties[key], seen)
		}
		out["properties"] = props
	}
	if schema.AdditionalProperties.Has != nil {
		out["additionalProperties"] = *schema.AdditionalProperties.Has
	} else if schema.AdditionalProperties.Schema != nil {
		out["additionalProperties"] = r.resolveSchemaRef(schema.AdditionalProperties.Schema, seen)
	}
	for key, val := range schema.Extensions {
		out[key] = cloneAny(val)
	}

	return out
}

// resolveSchemaRefs вычисляет производное значение на основе входных данных и текущего состояния.
func (r *OpenAPIResolver) resolveSchemaRefs(refs openapi3.SchemaRefs, seen map[*openapi3.Schema]struct{}) []any {
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, r.resolveSchemaRef(ref, seen))
	}
	return out
}

// sortedMediaTypeKeys выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func sortedMediaTypeKeys(content openapi3.Content) []string {
	if len(content) == 0 {
		return nil
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// preferredMedia выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func preferredMedia(content openapi3.Content) *openapi3.MediaType {
	if len(content) == 0 {
		return nil
	}
	if media, ok := content["application/json"]; ok && media != nil {
		return media
	}
	keys := sortedMediaTypeKeys(content)
	for _, key := range keys {
		if media := content[key]; media != nil {
			return media
		}
	}
	return nil
}

// extractExamples извлекает целевые данные из входного объекта с валидацией формата.
func extractExamples(media *openapi3.MediaType) any {
	if media == nil {
		return nil
	}
	if media.Example != nil {
		return cloneAny(media.Example)
	}
	if len(media.Examples) == 0 {
		return nil
	}
	keys := make([]string, 0, len(media.Examples))
	for key := range media.Examples {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		ex := media.Examples[key]
		if ex == nil || ex.Value == nil {
			continue
		}
		out[key] = cloneAny(ex.Value.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sortParams выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func sortParams(params []Param) {
	sort.Slice(params, func(i, j int) bool {
		if params[i].Name == params[j].Name {
			return params[i].In < params[j].In
		}
		return params[i].Name < params[j].Name
	})
}

// buildOperationURL собирает зависимость или конфигурационный объект для текущего слоя.
func buildOperationURL(baseURL, pathTemplate string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	baseURL = strings.TrimRight(baseURL, "/")
	pathTemplate = strings.TrimSpace(pathTemplate)
	if pathTemplate == "" {
		return baseURL
	}
	if strings.HasPrefix(pathTemplate, "/") {
		return baseURL + pathTemplate
	}
	return baseURL + "/" + pathTemplate
}

// buildExampleOperationURL собирает зависимость или конфигурационный объект для текущего слоя.
func buildExampleOperationURL(baseURL, pathTemplate string) string {
	if strings.Contains(pathTemplate, "{") {
		return ""
	}
	return buildOperationURL(baseURL, pathTemplate)
}

// firstServer возвращает первое подходящее значение по приоритетному правилу выбора.
func firstServer(servers []string) string {
	if len(servers) == 0 {
		return ""
	}
	return strings.TrimSpace(servers[0])
}

// parseStatusCode разбирает входные данные и возвращает нормализованное представление.
func parseStatusCode(value string) int {
	code, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return code
}

// compareStatus выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func compareStatus(a, b string) int {
	ai := parseStatusCode(a)
	bi := parseStatusCode(b)
	if ai == bi {
		if ai == 0 {
			return strings.Compare(a, b)
		}
		return 0
	}
	if ai == 0 {
		return 1
	}
	if bi == 0 {
		return -1
	}
	if ai < bi {
		return -1
	}
	return 1
}

// stringPtrValue выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// syntheticOperationID выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func syntheticOperationID(method, path string) string {
	normalizedPath := strings.Trim(path, "/")
	normalizedPath = strings.ReplaceAll(normalizedPath, "/", "_")
	normalizedPath = strings.ReplaceAll(normalizedPath, "{", "")
	normalizedPath = strings.ReplaceAll(normalizedPath, "}", "")
	if normalizedPath == "" {
		normalizedPath = "root"
	}
	return strings.ToLower(strings.TrimSpace(method)) + "_" + normalizedPath
}

// dedupeStrings выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// cloneViaJSON создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneViaJSON(value any) any {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return cloneAny(value)
	}
	var out any
	if err := json.Unmarshal(payload, &out); err != nil {
		return cloneAny(value)
	}
	return out
}

// cloneAny создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for key, val := range v {
			m[key] = cloneAny(val)
		}
		return m
	case []any:
		arr := make([]any, len(v))
		for i := range v {
			arr[i] = cloneAny(v[i])
		}
		return arr
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}
