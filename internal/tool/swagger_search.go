package tool

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// SwaggerSearchTool ищет операции по индексам загруженной swagger-спецификации.
type SwaggerSearchTool struct {
	store swagger.Store
}

// NewSwaggerSearchTool создает swagger.search tool.
func NewSwaggerSearchTool(store swagger.Store) *SwaggerSearchTool {
	return &SwaggerSearchTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerSearchTool) Name() string {
	return "swagger.search"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerSearchTool) Description() string {
	return "Searches swagger operations by operationId/path/summary/tags"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerSearchTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerSearchTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerSearchTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}
	parsed, err := parseSearchInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	endpoints, err := t.store.ListEndpoints(ctx)
	if err != nil {
		return errorResult("invalid_request", "failed to load swagger endpoints", map[string]any{"error": err.Error()}), nil
	}
	indexes := buildSearchIndexes(endpoints)
	includes := includeSet(parsed.Include)

	ranked := make([]rankedSearchResult, 0, len(endpoints))
	for _, endpoint := range endpoints {
		searchResult, ok := evaluateSearchEndpoint(endpoint, parsed, indexes)
		if !ok {
			continue
		}
		ranked = append(ranked, searchResult)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Endpoint.OperationID < ranked[j].Endpoint.OperationID
		}
		return ranked[i].Score > ranked[j].Score
	})

	results := make([]map[string]any, 0, len(ranked))
	if includes["endpoints"] {
		for _, item := range ranked {
			results = append(results, map[string]any{
				"operationId":  item.Endpoint.OperationID,
				"method":       item.Endpoint.Method,
				"pathTemplate": item.Endpoint.PathTemplate,
				"urlTemplate":  item.Endpoint.URLTemplate,
				"baseURL":      item.Endpoint.BaseURL,
				"summary":      item.Endpoint.Summary,
				"tags":         item.Endpoint.Tags,
				"matchReason":  item.MatchReason,
				"score":        item.Score,
			})
			if len(results) >= parsed.Limit {
				break
			}
		}
	}

	payload := map[string]any{
		"count": len(ranked),
		"filters": map[string]any{
			"query":   parsed.Query,
			"method":  parsed.Method,
			"tag":     parsed.Tag,
			"schema":  parsed.Schema,
			"status":  parsed.Status,
			"include": parsed.Include,
			"limit":   parsed.Limit,
		},
		"results": results,
	}
	if includes["schemas"] {
		payload["schemas"] = schemaMatchesPayload(parsed, indexes)
	}
	if includes["usage"] {
		payload["usage"] = usageMatchesPayload(parsed, indexes)
	}
	return okResult(payload), nil
}

// rankedSearchResult хранит промежуточные данные инструмента между этапами подготовки и валидации.
type rankedSearchResult struct {
	Endpoint    swagger.ResolvedOperation
	Score       float64
	MatchReason []string
}

// schemaUsageMatch хранит промежуточные данные инструмента между этапами подготовки и валидации.
type schemaUsageMatch struct {
	Request  bool
	Response bool
}

// statusUsageMatch хранит промежуточные данные инструмента между этапами подготовки и валидации.
type statusUsageMatch struct {
	Success bool
	Error   bool
}

// searchIndexes хранит промежуточные данные инструмента между этапами подготовки и валидации.
type searchIndexes struct {
	SchemaUsage  map[string]map[string]schemaUsageMatch
	SchemaNames  map[string]string
	StatusUsage  map[int]map[string]statusUsageMatch
	OperationMap map[string]swagger.ResolvedOperation
}

// buildSearchIndexes собирает зависимость или конфигурационный объект для текущего слоя.
func buildSearchIndexes(endpoints []swagger.ResolvedOperation) searchIndexes {
	idx := searchIndexes{
		SchemaUsage:  make(map[string]map[string]schemaUsageMatch),
		SchemaNames:  make(map[string]string),
		StatusUsage:  make(map[int]map[string]statusUsageMatch),
		OperationMap: make(map[string]swagger.ResolvedOperation),
	}

	for _, endpoint := range endpoints {
		opID := strings.TrimSpace(endpoint.OperationID)
		if opID == "" {
			continue
		}
		idx.OperationMap[opID] = endpoint

		requestSchemas := collectSchemaNames(endpoint.Request.BodySchema)
		for _, schemaName := range requestSchemas {
			addSchemaUsage(idx, schemaName, opID, true, false)
		}

		for _, response := range endpoint.Responses.Success {
			addStatusUsage(idx, response.Status, opID, true, false)
			for _, schemaName := range collectSchemaNames(response.BodySchema) {
				addSchemaUsage(idx, schemaName, opID, false, true)
			}
		}
		for _, response := range endpoint.Responses.Errors {
			addStatusUsage(idx, response.Status, opID, false, true)
			for _, schemaName := range collectSchemaNames(response.BodySchema) {
				addSchemaUsage(idx, schemaName, opID, false, true)
			}
		}
	}
	return idx
}

// addSchemaUsage выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func addSchemaUsage(idx searchIndexes, schemaName, operationID string, request, response bool) {
	key := strings.ToLower(strings.TrimSpace(schemaName))
	if key == "" {
		return
	}
	if _, ok := idx.SchemaUsage[key]; !ok {
		idx.SchemaUsage[key] = make(map[string]schemaUsageMatch)
	}
	if _, ok := idx.SchemaNames[key]; !ok {
		idx.SchemaNames[key] = strings.TrimSpace(schemaName)
	}
	usage := idx.SchemaUsage[key][operationID]
	usage.Request = usage.Request || request
	usage.Response = usage.Response || response
	idx.SchemaUsage[key][operationID] = usage
}

// addStatusUsage выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func addStatusUsage(idx searchIndexes, status int, operationID string, success, err bool) {
	if status <= 0 {
		return
	}
	if _, ok := idx.StatusUsage[status]; !ok {
		idx.StatusUsage[status] = make(map[string]statusUsageMatch)
	}
	usage := idx.StatusUsage[status][operationID]
	usage.Success = usage.Success || success
	usage.Error = usage.Error || err
	idx.StatusUsage[status][operationID] = usage
}

// evaluateSearchEndpoint выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func evaluateSearchEndpoint(endpoint swagger.ResolvedOperation, parsed searchInput, idx searchIndexes) (rankedSearchResult, bool) {
	score := 1.0
	reasons := make([]string, 0)

	if parsed.Method != "" && strings.ToUpper(endpoint.Method) != parsed.Method {
		return rankedSearchResult{}, false
	}
	if parsed.Method != "" {
		score += 0.4
		reasons = append(reasons, "method filter matched")
	}

	if parsed.Tag != "" {
		if !containsTag(endpoint.Tags, parsed.Tag) {
			return rankedSearchResult{}, false
		}
		score += 0.4
		reasons = append(reasons, "tag filter matched")
	}

	if parsed.Query != "" {
		queryScore, queryReasons := queryMatchScore(endpoint, parsed.Query)
		if queryScore <= 0 {
			return rankedSearchResult{}, false
		}
		score += queryScore
		reasons = append(reasons, queryReasons...)
	}

	if parsed.Schema != "" {
		usage, ok := schemaUsageForEndpoint(idx, parsed.Schema, endpoint.OperationID)
		if !ok {
			return rankedSearchResult{}, false
		}
		switch {
		case usage.Request && usage.Response:
			reasons = append(reasons, "schema used in request and response")
			score += 5
		case usage.Request:
			reasons = append(reasons, "schema used in request body")
			score += 4
		case usage.Response:
			reasons = append(reasons, "schema used in response body")
			score += 4
		}
	}

	if parsed.Status > 0 {
		usage, ok := statusUsageForEndpoint(idx, parsed.Status, endpoint.OperationID)
		if !ok {
			return rankedSearchResult{}, false
		}
		if usage.Error {
			reasons = append(reasons, "matches error response status "+strconv.Itoa(parsed.Status))
			score += 3.5
		}
		if usage.Success {
			reasons = append(reasons, "matches success response status "+strconv.Itoa(parsed.Status))
			score += 2.0
		}
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "matched by default listing")
	}

	return rankedSearchResult{
		Endpoint:    endpoint,
		Score:       score,
		MatchReason: uniqueStrings(reasons),
	}, true
}

// queryMatchScore выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func queryMatchScore(endpoint swagger.ResolvedOperation, query string) (float64, []string) {
	reasons := make([]string, 0)
	score := 0.0
	if strings.Contains(strings.ToLower(endpoint.OperationID), query) {
		score += 3.0
		reasons = append(reasons, "query matched operationId")
	}
	if strings.Contains(strings.ToLower(endpoint.PathTemplate), query) {
		score += 2.0
		reasons = append(reasons, "query matched pathTemplate")
	}
	if strings.Contains(strings.ToLower(endpoint.Summary), query) {
		score += 2.0
		reasons = append(reasons, "query matched summary")
	}
	if strings.Contains(strings.ToLower(endpoint.Description), query) {
		score += 1.5
		reasons = append(reasons, "query matched description")
	}
	for _, tag := range endpoint.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			score += 1.0
			reasons = append(reasons, "query matched tag")
			break
		}
	}
	return score, uniqueStrings(reasons)
}

// schemaUsageForEndpoint выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaUsageForEndpoint(idx searchIndexes, schema, operationID string) (schemaUsageMatch, bool) {
	usageByOp, ok := idx.SchemaUsage[strings.ToLower(strings.TrimSpace(schema))]
	if !ok {
		return schemaUsageMatch{}, false
	}
	usage, ok := usageByOp[strings.TrimSpace(operationID)]
	return usage, ok
}

// statusUsageForEndpoint выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func statusUsageForEndpoint(idx searchIndexes, status int, operationID string) (statusUsageMatch, bool) {
	usageByOp, ok := idx.StatusUsage[status]
	if !ok {
		return statusUsageMatch{}, false
	}
	usage, ok := usageByOp[strings.TrimSpace(operationID)]
	return usage, ok
}

// schemaMatchesPayload выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaMatchesPayload(parsed searchInput, idx searchIndexes) []map[string]any {
	out := make([]map[string]any, 0)
	if parsed.Schema != "" {
		key := strings.ToLower(strings.TrimSpace(parsed.Schema))
		usageByOp := idx.SchemaUsage[key]
		if len(usageByOp) == 0 {
			return out
		}
		out = append(out, map[string]any{
			"schema":    idx.SchemaNames[key],
			"endpoints": schemaUsageEndpoints(usageByOp),
		})
		return out
	}

	if parsed.Query == "" {
		return out
	}
	for key, schemaName := range idx.SchemaNames {
		if !strings.Contains(strings.ToLower(schemaName), parsed.Query) && !strings.Contains(key, parsed.Query) {
			continue
		}
		usageByOp := idx.SchemaUsage[key]
		out = append(out, map[string]any{
			"schema":    schemaName,
			"endpoints": schemaUsageEndpoints(usageByOp),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return valueAsString(out[i]["schema"]) < valueAsString(out[j]["schema"])
	})
	return out
}

// usageMatchesPayload выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func usageMatchesPayload(parsed searchInput, idx searchIndexes) map[string]any {
	out := map[string]any{
		"summary": map[string]any{
			"indexedSchemas":  len(idx.SchemaUsage),
			"indexedStatuses": len(idx.StatusUsage),
		},
	}
	if parsed.Schema != "" {
		key := strings.ToLower(strings.TrimSpace(parsed.Schema))
		usageByOp := idx.SchemaUsage[key]
		out["schema"] = map[string]any{
			"name":      idx.SchemaNames[key],
			"endpoints": schemaUsageEndpoints(usageByOp),
		}
	}
	if parsed.Status > 0 {
		out["status"] = map[string]any{
			"code":      parsed.Status,
			"endpoints": statusUsageEndpoints(idx.StatusUsage[parsed.Status]),
		}
	}
	return out
}

// schemaUsageEndpoints выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaUsageEndpoints(usageByOp map[string]schemaUsageMatch) []map[string]any {
	if len(usageByOp) == 0 {
		return nil
	}
	ids := make([]string, 0, len(usageByOp))
	for id := range usageByOp {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		usage := usageByOp[id]
		out = append(out, map[string]any{
			"operationId": id,
			"request":     usage.Request,
			"response":    usage.Response,
		})
	}
	return out
}

// statusUsageEndpoints выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func statusUsageEndpoints(usageByOp map[string]statusUsageMatch) []map[string]any {
	if len(usageByOp) == 0 {
		return nil
	}
	ids := make([]string, 0, len(usageByOp))
	for id := range usageByOp {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		usage := usageByOp[id]
		out = append(out, map[string]any{
			"operationId": id,
			"success":     usage.Success,
			"error":       usage.Error,
		})
	}
	return out
}

// includeSet обновляет соответствующий счетчик или метрику наблюдаемости.
func includeSet(items []string) map[string]bool {
	set := map[string]bool{
		"endpoints": false,
		"schemas":   false,
		"usage":     false,
	}
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item))
		if _, ok := set[key]; ok {
			set[key] = true
		}
	}
	if !set["endpoints"] && !set["schemas"] && !set["usage"] {
		set["endpoints"] = true
	}
	return set
}

// collectSchemaNames выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func collectSchemaNames(value any) []string {
	names := map[string]struct{}{}
	collectSchemaNamesNode(value, names)
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectSchemaNamesNode выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func collectSchemaNamesNode(value any, names map[string]struct{}) {
	switch node := value.(type) {
	case map[string]any:
		for _, marker := range []string{"x-originRef", "x-circularRef", "x-unresolvedRef"} {
			if name, ok := schemaNameFromRef(valueAsString(node[marker])); ok {
				names[name] = struct{}{}
			}
		}
		for _, child := range node {
			collectSchemaNamesNode(child, names)
		}
	case []any:
		for _, child := range node {
			collectSchemaNamesNode(child, names)
		}
	}
}

// schemaNameFromRef выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaNameFromRef(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	const marker = "/components/schemas/"
	idx := strings.Index(ref, marker)
	if idx < 0 {
		return "", false
	}
	name := strings.TrimSpace(ref[idx+len(marker):])
	if name == "" {
		return "", false
	}
	if pos := strings.Index(name, "/"); pos >= 0 {
		name = name[:pos]
	}
	name = strings.ReplaceAll(name, "~1", "/")
	name = strings.ReplaceAll(name, "~0", "~")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	return name, true
}

// matchesQuery выполняет проверку соответствия по правилам текущего модуля.
func matchesQuery(endpoint swagger.ResolvedOperation, query string) bool {
	if strings.Contains(strings.ToLower(endpoint.OperationID), query) {
		return true
	}
	if strings.Contains(strings.ToLower(endpoint.PathTemplate), query) {
		return true
	}
	if strings.Contains(strings.ToLower(endpoint.Summary), query) {
		return true
	}
	if strings.Contains(strings.ToLower(endpoint.Description), query) {
		return true
	}
	for _, tag := range endpoint.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

// containsTag выполняет проверку соответствия по правилам текущего модуля.
func containsTag(tags []string, tag string) bool {
	for _, item := range tags {
		if strings.EqualFold(strings.TrimSpace(item), tag) {
			return true
		}
	}
	return false
}
