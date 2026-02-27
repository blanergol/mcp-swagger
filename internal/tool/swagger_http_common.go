package tool

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// pathParamPattern хранит служебное значение, используемое внутри текущего пакета.
var pathParamPattern = regexp.MustCompile(`\{([^{}]+)\}`)

// toolError хранит промежуточные данные инструмента между этапами подготовки и валидации.
type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// okResult выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func okResult(data any) map[string]any {
	return map[string]any{
		"ok":    true,
		"data":  data,
		"error": nil,
	}
}

// errorResult выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func errorResult(code, message string, details any) map[string]any {
	return map[string]any{
		"ok":   false,
		"data": nil,
		"error": toolError{
			Code:    strings.TrimSpace(code),
			Message: strings.TrimSpace(message),
			Details: details,
		},
	}
}

// toolInputParams хранит промежуточные данные инструмента между этапами подготовки и валидации.
type toolInputParams struct {
	Path    map[string]any
	Query   map[string]any
	Headers map[string]any
	Body    any
}

// toolInputEnvelope хранит промежуточные данные инструмента между этапами подготовки и валидации.
type toolInputEnvelope struct {
	OperationID string
	Params      toolInputParams
	ContentType string
	BaseURL     string
}

// executeInput хранит промежуточные данные инструмента между этапами подготовки и валидации.
type executeInput struct {
	OperationID    string
	PathParams     map[string]string
	QueryParams    map[string]string
	Headers        map[string]string
	Body           any
	ContentType    string
	BaseURL        string
	ConfirmationID string
}

// searchInput хранит промежуточные данные инструмента между этапами подготовки и валидации.
type searchInput struct {
	Query   string
	Method  string
	Tag     string
	Limit   int
	Include []string
	Schema  string
	Status  int
}

// planCallInput хранит промежуточные данные инструмента между этапами подготовки и валидации.
type planCallInput struct {
	OperationID string
	Query       string
	Goal        string
}

// validateResponseInput описывает служебную структуру данных для передачи между шагами обработки.
type validateResponseInput struct {
	OperationID  string
	Status       int
	Headers      map[string]string
	ContentType  string
	BodyEncoding string
	Body         any
}

// responseBodyEnvelope описывает служебную структуру данных для передачи между шагами обработки.
type responseBodyEnvelope struct {
	ContentType  string `json:"contentType"`
	BodyEncoding string `json:"bodyEncoding"`
	Body         any    `json:"body"`
}

// preparedRequest описывает служебную структуру данных для передачи между шагами обработки.
type preparedRequest struct {
	OperationID string
	Method      string
	Path        string
	URL         string

	PathParams  map[string]string
	QueryParams map[string]string
	Headers     map[string]string

	ContentType string
	BodyInput   any
	BodyBytes   []byte
}

// prepareOptions описывает набор параметров, влияющих на поведение компонента.
type prepareOptions struct {
	BaseURL       string
	AllowNoBase   bool
	MaxBodyBytes  int64
	DefaultAgent  string
	DefaultAccept string
}

// parseExecuteInput разбирает входные данные и возвращает нормализованное представление.
func parseExecuteInput(input any) (executeInput, error) {
	envelope, err := parseToolInputEnvelope(input, true)
	if err != nil {
		return executeInput{}, err
	}
	inMap, mapErr := toAnyMap(input)
	if mapErr != nil {
		return executeInput{}, mapErr
	}
	if envelope.Params.Query == nil {
		queryMap, queryErr := toAnyMapOrNil(inMap["query"])
		if queryErr == nil {
			envelope.Params.Query = queryMap
		}
	}

	pathParams, err := valueAsStringMap(envelope.Params.Path)
	if err != nil {
		return executeInput{}, fmt.Errorf("params.path: %w", err)
	}
	queryParams, err := valueAsStringMap(envelope.Params.Query)
	if err != nil {
		return executeInput{}, fmt.Errorf("params.query: %w", err)
	}
	headers, err := valueAsStringMap(envelope.Params.Headers)
	if err != nil {
		return executeInput{}, fmt.Errorf("params.headers: %w", err)
	}

	contentType := envelope.ContentType
	if contentType == "" {
		if value, ok := findHeader(headers, "Content-Type"); ok {
			contentType = strings.TrimSpace(value)
		}
	}

	confirmationID := strings.TrimSpace(valueAsString(inMap["confirmationId"]))
	if confirmationID == "" {
		confirmationID = strings.TrimSpace(valueAsString(inMap["confirmationID"]))
	}
	if confirmationID == "" {
		confirmationID = strings.TrimSpace(valueAsString(inMap["confirmationToken"]))
	}
	if confirmationID == "" {
		confirmationID = strings.TrimSpace(valueAsString(mapValue(envelope.Params.Query, "confirmationId")))
	}
	if confirmationID == "" {
		confirmationID = strings.TrimSpace(valueAsString(mapValue(envelope.Params.Query, "confirmationToken")))
	}

	return executeInput{
		OperationID:    envelope.OperationID,
		PathParams:     pathParams,
		QueryParams:    queryParams,
		Headers:        headers,
		Body:           envelope.Params.Body,
		ContentType:    contentType,
		BaseURL:        envelope.BaseURL,
		ConfirmationID: confirmationID,
	}, nil
}

// parseToolInputEnvelope разбирает входные данные и возвращает нормализованное представление.
func parseToolInputEnvelope(input any, requireOperationID bool) (toolInputEnvelope, error) {
	inMap, err := toAnyMap(input)
	if err != nil {
		return toolInputEnvelope{}, err
	}

	operationID := strings.TrimSpace(valueAsString(inMap["operationId"]))
	if operationID == "" {
		operationID = strings.TrimSpace(valueAsString(inMap["operationID"]))
	}
	if requireOperationID && operationID == "" {
		return toolInputEnvelope{}, errors.New("operationId is required")
	}

	paramsMap := map[string]any{}
	if rawParams, ok := inMap["params"]; ok && rawParams != nil {
		paramsMap, err = toAnyMap(rawParams)
		if err != nil {
			return toolInputEnvelope{}, fmt.Errorf("params must be an object")
		}
	}

	pathRaw := paramOrLegacy(paramsMap, "path", inMap["pathParams"])
	queryRaw := paramOrLegacy(paramsMap, "query", inMap["queryParams"])
	headersRaw := paramOrLegacy(paramsMap, "headers", inMap["headers"])
	bodyRaw := paramOrLegacy(paramsMap, "body", inMap["body"])

	pathMap, err := toAnyMapOrNil(pathRaw)
	if err != nil {
		return toolInputEnvelope{}, fmt.Errorf("params.path: %w", err)
	}
	queryMap, err := toAnyMapOrNil(queryRaw)
	if err != nil {
		return toolInputEnvelope{}, fmt.Errorf("params.query: %w", err)
	}
	headersMap, err := toAnyMapOrNil(headersRaw)
	if err != nil {
		return toolInputEnvelope{}, fmt.Errorf("params.headers: %w", err)
	}

	contentType := strings.TrimSpace(valueAsString(inMap["contentType"]))
	if contentType == "" {
		contentType = strings.TrimSpace(valueAsString(inMap["content_type"]))
	}

	baseURL := strings.TrimSpace(valueAsString(inMap["baseURL"]))
	if baseURL == "" {
		baseURL = strings.TrimSpace(valueAsString(inMap["baseUrl"]))
	}

	return toolInputEnvelope{
		OperationID: operationID,
		Params: toolInputParams{
			Path:    pathMap,
			Query:   queryMap,
			Headers: headersMap,
			Body:    bodyRaw,
		},
		ContentType: contentType,
		BaseURL:     baseURL,
	}, nil
}

// parseSearchInput разбирает входные данные и возвращает нормализованное представление.
func parseSearchInput(input any) (searchInput, error) {
	envelope, err := parseToolInputEnvelope(input, false)
	if err != nil {
		return searchInput{}, err
	}
	inMap, err := toAnyMap(input)
	if err != nil {
		return searchInput{}, err
	}

	queryRaw := firstNonNil(mapValue(envelope.Params.Query, "query"), inMap["query"], mapValue(envelope.Params.Query, "q"), inMap["q"])
	methodRaw := firstNonNil(mapValue(envelope.Params.Query, "method"), inMap["method"])
	tagRaw := firstNonNil(mapValue(envelope.Params.Query, "tag"), inMap["tag"])
	limitRaw := firstNonNil(mapValue(envelope.Params.Query, "limit"), inMap["limit"])
	includeRaw := firstNonNil(mapValue(envelope.Params.Query, "include"), inMap["include"])
	schemaRaw := firstNonNil(mapValue(envelope.Params.Query, "schema"), inMap["schema"])
	statusRaw := firstNonNil(mapValue(envelope.Params.Query, "status"), inMap["status"])

	limit := 20
	if limitRaw != nil {
		parsed, parseErr := parseStatusCode(limitRaw)
		if parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	status := 0
	if statusRaw != nil {
		parsed, parseErr := parseStatusCode(statusRaw)
		if parseErr == nil && parsed > 0 {
			status = parsed
		}
	}
	include := parseStringListAny(includeRaw)
	if len(include) == 0 {
		include = []string{"endpoints"}
	}

	return searchInput{
		Query:   strings.ToLower(strings.TrimSpace(valueAsString(queryRaw))),
		Method:  strings.ToUpper(strings.TrimSpace(valueAsString(methodRaw))),
		Tag:     strings.ToLower(strings.TrimSpace(valueAsString(tagRaw))),
		Limit:   limit,
		Include: include,
		Schema:  strings.TrimSpace(valueAsString(schemaRaw)),
		Status:  status,
	}, nil
}

// parseStringListAny разбирает входные данные и возвращает нормализованное представление.
func parseStringListAny(value any) []string {
	if value == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(items ...string) {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			lower := strings.ToLower(item)
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			out = append(out, lower)
		}
	}

	switch v := value.(type) {
	case string:
		parts := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		add(parts...)
	case []string:
		add(v...)
	case []any:
		for _, item := range v {
			add(valueAsString(item))
		}
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var parsed []any
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil
		}
		for _, item := range parsed {
			add(valueAsString(item))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parsePlanCallInput разбирает входные данные и возвращает нормализованное представление.
func parsePlanCallInput(input any) (planCallInput, error) {
	envelope, err := parseToolInputEnvelope(input, false)
	if err != nil {
		return planCallInput{}, err
	}
	inMap, err := toAnyMap(input)
	if err != nil {
		return planCallInput{}, err
	}

	query := strings.TrimSpace(valueAsString(firstNonNil(
		mapValue(envelope.Params.Query, "query"),
		inMap["query"],
		mapValue(envelope.Params.Query, "q"),
		inMap["q"],
	)))
	goal := strings.TrimSpace(valueAsString(firstNonNil(
		mapValue(envelope.Params.Query, "goal"),
		inMap["goal"],
		mapValueAsString(envelope.Params.Body, "goal"),
	)))

	return planCallInput{
		OperationID: strings.TrimSpace(envelope.OperationID),
		Query:       query,
		Goal:        goal,
	}, nil
}

// parseValidateResponseInput разбирает входные данные и возвращает нормализованное представление.
func parseValidateResponseInput(input any) (validateResponseInput, error) {
	envelope, err := parseToolInputEnvelope(input, true)
	if err != nil {
		return validateResponseInput{}, err
	}
	inMap, err := toAnyMap(input)
	if err != nil {
		return validateResponseInput{}, err
	}

	statusRaw := firstNonNil(mapValue(envelope.Params.Query, "status"), inMap["status"])
	status, err := parseStatusCode(statusRaw)
	if err != nil || status <= 0 {
		return validateResponseInput{}, errors.New("status must be a positive HTTP status code")
	}

	headers, err := valueAsStringMap(envelope.Params.Headers)
	if err != nil {
		return validateResponseInput{}, fmt.Errorf("params.headers: %w", err)
	}
	contentType := strings.TrimSpace(envelope.ContentType)
	if contentType == "" {
		if value, ok := findHeader(headers, "Content-Type"); ok {
			contentType = strings.TrimSpace(value)
		}
	}
	bodyEncoding := strings.ToLower(strings.TrimSpace(valueAsString(inMap["bodyEncoding"])))

	return validateResponseInput{
		OperationID:  envelope.OperationID,
		Status:       status,
		Headers:      headers,
		ContentType:  contentType,
		BodyEncoding: bodyEncoding,
		Body:         envelope.Params.Body,
	}, nil
}

// toAnyMapOrNil выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toAnyMapOrNil(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	m, err := toAnyMap(value)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

// firstNonNil возвращает первое подходящее значение по приоритетному правилу выбора.
func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// paramOrLegacy выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func paramOrLegacy(params map[string]any, key string, legacy any) any {
	if params != nil {
		if value, exists := params[key]; exists {
			return value
		}
	}
	return legacy
}

// mapValue выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mapValue(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	return values[key]
}

// mapValueAsString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mapValueAsString(value any, key string) string {
	values, err := toAnyMap(value)
	if err != nil {
		return ""
	}
	return valueAsString(values[key])
}

// prepareEndpointRequest выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func prepareEndpointRequest(endpoint swagger.ResolvedOperation, input executeInput, opts prepareOptions) (preparedRequest, error) {
	method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
	if method == "" {
		return preparedRequest{}, errors.New("endpoint method is empty")
	}

	path, missing := expandPathTemplate(endpoint.PathTemplate, input.PathParams)
	if len(missing) > 0 {
		return preparedRequest{}, fmt.Errorf("missing path params: %s", strings.Join(missing, ", "))
	}

	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(input.BaseURL)
	}
	if baseURL == "" && len(endpoint.Servers) > 0 {
		baseURL = strings.TrimSpace(endpoint.Servers[0])
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(endpoint.BaseURL)
	}
	if baseURL == "" {
		baseURL = inferBaseURLFromEndpointURL(endpoint)
	}

	finalURL, err := buildTargetURL(baseURL, path, input.QueryParams, opts.AllowNoBase)
	if err != nil {
		return preparedRequest{}, err
	}

	headers := copyStringMap(input.Headers)
	if len(headers) == 0 {
		headers = map[string]string{}
	}
	if opts.DefaultAgent != "" {
		if _, exists := findHeader(headers, "User-Agent"); !exists {
			headers["User-Agent"] = opts.DefaultAgent
		}
	}
	if opts.DefaultAccept != "" {
		if _, exists := findHeader(headers, "Accept"); !exists {
			headers["Accept"] = opts.DefaultAccept
		}
	}

	bodyBytes, contentType, bodyForValidation, err := encodeRequestBody(input.Body, input.ContentType)
	if err != nil {
		return preparedRequest{}, err
	}
	if contentType != "" && len(bodyBytes) > 0 {
		if _, exists := findHeader(headers, "Content-Type"); !exists {
			headers["Content-Type"] = contentType
		}
	}
	if opts.MaxBodyBytes > 0 && int64(len(bodyBytes)) > opts.MaxBodyBytes {
		return preparedRequest{}, fmt.Errorf("request payload exceeds MAX_REQUEST_BYTES=%d", opts.MaxBodyBytes)
	}

	return preparedRequest{
		OperationID: endpoint.OperationID,
		Method:      method,
		Path:        path,
		URL:         finalURL,
		PathParams:  copyStringMap(input.PathParams),
		QueryParams: copyStringMap(input.QueryParams),
		Headers:     headers,
		ContentType: contentType,
		BodyInput:   bodyForValidation,
		BodyBytes:   bodyBytes,
	}, nil
}

// inferBaseURLFromEndpointURL выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func inferBaseURLFromEndpointURL(endpoint swagger.ResolvedOperation) string {
	if base := strings.TrimSpace(endpoint.BaseURL); base != "" {
		return base
	}
	rawURL := strings.TrimSpace(endpoint.URLTemplate)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	base := parsed.Scheme + "://" + parsed.Host
	if parsed.Path != "" && endpoint.PathTemplate != "" && strings.HasSuffix(parsed.Path, endpoint.PathTemplate) {
		basePath := strings.TrimSuffix(parsed.Path, endpoint.PathTemplate)
		basePath = strings.TrimSuffix(basePath, "/")
		if basePath != "" {
			base += basePath
		}
	}
	return base
}

// buildTargetURL собирает зависимость или конфигурационный объект для текущего слоя.
func buildTargetURL(baseURL, path string, query map[string]string, allowNoBase bool) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		if allowNoBase {
			if len(query) == 0 {
				return path, nil
			}
			q := url.Values{}
			for key, value := range query {
				q.Set(key, value)
			}
			return path + "?" + q.Encode(), nil
		}
		return "", errors.New("no base URL")
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	resolved := base.ResolveReference(rel)
	q := resolved.Query()
	for key, value := range query {
		q.Set(key, value)
	}
	resolved.RawQuery = q.Encode()
	return resolved.String(), nil
}

// expandPathTemplate выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func expandPathTemplate(path string, params map[string]string) (string, []string) {
	missing := make([]string, 0)
	resolved := pathParamPattern.ReplaceAllStringFunc(path, func(segment string) string {
		matches := pathParamPattern.FindStringSubmatch(segment)
		if len(matches) != 2 {
			return segment
		}
		name := strings.TrimSpace(matches[1])
		value, ok := params[name]
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, name)
			return segment
		}
		return url.PathEscape(value)
	})
	return resolved, uniqueStrings(missing)
}

// encodeRequestBody выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func encodeRequestBody(body any, contentType string) ([]byte, string, any, error) {
	contentType = strings.TrimSpace(contentType)
	if body == nil {
		return nil, contentType, nil, nil
	}

	switch value := body.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if contentType == "" && looksLikeJSONString(trimmed) {
			contentType = "application/json"
		}
		if strings.Contains(strings.ToLower(contentType), "json") {
			var parsed any
			if err := json.Unmarshal([]byte(value), &parsed); err != nil {
				return nil, "", nil, fmt.Errorf("body must be valid JSON for content-type %q: %w", contentType, err)
			}
			payload, err := json.Marshal(parsed)
			if err != nil {
				return nil, "", nil, err
			}
			return payload, contentType, parsed, nil
		}
		if contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		return []byte(value), contentType, value, nil
	case []byte:
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if strings.Contains(strings.ToLower(contentType), "json") {
			var parsed any
			if err := json.Unmarshal(value, &parsed); err == nil {
				payload, marshalErr := json.Marshal(parsed)
				if marshalErr != nil {
					return nil, "", nil, marshalErr
				}
				return payload, contentType, parsed, nil
			}
		}
		return append([]byte(nil), value...), contentType, map[string]any{
			"encoding": "base64",
			"data":     base64.StdEncoding.EncodeToString(value),
		}, nil
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, "", nil, fmt.Errorf("encode request body: %w", err)
		}
		if contentType == "" {
			contentType = "application/json"
		}
		var parsed any
		if err := json.Unmarshal(payload, &parsed); err != nil {
			parsed = value
		}
		return payload, contentType, parsed, nil
	}
}

// validatePreparedRequest выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validatePreparedRequest(endpoint swagger.ResolvedOperation, req preparedRequest) []string {
	issues := make([]string, 0)

	if !strings.EqualFold(strings.TrimSpace(endpoint.Method), strings.TrimSpace(req.Method)) {
		issues = append(issues, fmt.Sprintf("method mismatch: endpoint=%s request=%s", endpoint.Method, req.Method))
	}

	issues = append(issues, validateRequiredParams(endpoint.PathParams, req.PathParams, "path")...)
	issues = append(issues, validateRequiredParams(endpoint.QueryParams, req.QueryParams, "query")...)
	issues = append(issues, validateRequiredHeaders(endpoint.HeaderParams, req.Headers)...)

	schema := endpoint.Request.BodySchema
	if schema == nil {
		if req.BodyInput != nil {
			issues = append(issues, "request body provided but operation does not define request body")
		}
		return uniqueStrings(issues)
	}
	if req.BodyInput == nil {
		return uniqueStrings(issues)
	}

	issues = append(issues, validateValueAgainstSchema(req.BodyInput, schema, "body")...)
	return uniqueStrings(issues)
}

// validateResponse выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateResponse(endpoint swagger.ResolvedOperation, status int, body any) []string {
	schema, found := responseSchemaForStatus(endpoint, status)
	if !found {
		return []string{fmt.Sprintf("status %d is not defined in operation responses", status)}
	}
	if schema == nil || body == nil {
		return nil
	}
	return uniqueStrings(validateValueAgainstSchema(body, schema, "response.body"))
}

// responseSchemaForStatus выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func responseSchemaForStatus(endpoint swagger.ResolvedOperation, status int) (any, bool) {
	for _, success := range endpoint.Responses.Success {
		if success.Status == status {
			return success.BodySchema, true
		}
	}
	for _, failure := range endpoint.Responses.Errors {
		if failure.Status == status {
			return failure.BodySchema, true
		}
	}
	for _, success := range endpoint.Responses.Success {
		if success.Status == 0 {
			return success.BodySchema, true
		}
	}
	for _, failure := range endpoint.Responses.Errors {
		if failure.Status == 0 {
			return failure.BodySchema, true
		}
	}
	return nil, false
}

// validateRequiredParams выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateRequiredParams(defs []swagger.Param, provided map[string]string, location string) []string {
	if len(defs) == 0 {
		return nil
	}
	issues := make([]string, 0)
	for _, def := range defs {
		if !def.Required {
			continue
		}
		value, ok := provided[def.Name]
		if !ok || strings.TrimSpace(value) == "" {
			issues = append(issues, fmt.Sprintf("missing required %s parameter %q", location, def.Name))
		}
	}
	return issues
}

// validateRequiredHeaders выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateRequiredHeaders(defs []swagger.Param, provided map[string]string) []string {
	if len(defs) == 0 {
		return nil
	}
	lower := make(map[string]string, len(provided))
	for key, value := range provided {
		lower[strings.ToLower(strings.TrimSpace(key))] = value
	}
	issues := make([]string, 0)
	for _, def := range defs {
		if !def.Required {
			continue
		}
		value, ok := lower[strings.ToLower(strings.TrimSpace(def.Name))]
		if !ok || strings.TrimSpace(value) == "" {
			issues = append(issues, fmt.Sprintf("missing required header parameter %q", def.Name))
		}
	}
	return issues
}

// validateValueAgainstSchema выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateValueAgainstSchema(value any, schema any, path string) []string {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	issues := make([]string, 0)

	if allOf, ok := schemaMap["allOf"].([]any); ok && len(allOf) > 0 {
		for i, branch := range allOf {
			issues = append(issues, validateValueAgainstSchema(value, branch, fmt.Sprintf("%s.allOf[%d]", path, i))...)
		}
	}
	if anyOf, ok := schemaMap["anyOf"].([]any); ok && len(anyOf) > 0 {
		if !matchesAtLeastOne(value, anyOf) {
			issues = append(issues, fmt.Sprintf("%s does not match anyOf constraints", path))
		}
	}
	if oneOf, ok := schemaMap["oneOf"].([]any); ok && len(oneOf) > 0 {
		matches := 0
		for _, branch := range oneOf {
			if len(validateValueAgainstSchema(value, branch, path)) == 0 {
				matches++
			}
		}
		if matches != 1 {
			issues = append(issues, fmt.Sprintf("%s must match exactly one schema from oneOf", path))
		}
	}

	typeName := schemaType(schemaMap)
	if typeName != "" {
		if err := ensureType(typeName, value); err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", path, err))
			return uniqueStrings(issues)
		}
	}

	if typeName == "object" {
		obj, _ := value.(map[string]any)
		required := schemaRequired(schemaMap)
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				issues = append(issues, fmt.Sprintf("%s.%s is required", path, key))
			}
		}
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			for key, childSchema := range props {
				child, exists := obj[key]
				if !exists {
					continue
				}
				issues = append(issues, validateValueAgainstSchema(child, childSchema, path+"."+key)...)
			}
		}
	}
	if typeName == "array" {
		arr, _ := value.([]any)
		if itemSchema, ok := schemaMap["items"]; ok {
			for i, item := range arr {
				issues = append(issues, validateValueAgainstSchema(item, itemSchema, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}

	if enumRaw, ok := schemaMap["enum"].([]any); ok && len(enumRaw) > 0 {
		if !enumContains(enumRaw, value) {
			issues = append(issues, fmt.Sprintf("%s is not in enum", path))
		}
	}

	return uniqueStrings(issues)
}

// matchesAtLeastOne выполняет проверку соответствия по правилам текущего модуля.
func matchesAtLeastOne(value any, schemas []any) bool {
	for _, schema := range schemas {
		if len(validateValueAgainstSchema(value, schema, "value")) == 0 {
			return true
		}
	}
	return false
}

// schemaType выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaType(schema map[string]any) string {
	raw, ok := schema["type"]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(value))
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if ok && strings.TrimSpace(text) != "" {
				return strings.ToLower(strings.TrimSpace(text))
			}
		}
	}
	return ""
}

// ensureType выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func ensureType(expected string, value any) error {
	switch expected {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("expected object")
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("expected array")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		if !isInteger(value) {
			return fmt.Errorf("expected integer")
		}
	case "number":
		if !isNumber(value) {
			return fmt.Errorf("expected number")
		}
	}
	return nil
}

// isNumber возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isNumber(value any) bool {
	switch value.(type) {
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

// isInteger возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isInteger(value any) bool {
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return v == float64(int64(v))
	case float32:
		return v == float32(int64(v))
	case json.Number:
		_, err := v.Int64()
		return err == nil
	default:
		return false
	}
}

// schemaRequired выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func schemaRequired(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return uniqueStrings(out)
	case []string:
		return uniqueStrings(value)
	default:
		return nil
	}
}

// enumContains выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func enumContains(enum []any, value any) bool {
	for _, item := range enum {
		if fmt.Sprintf("%v", item) == fmt.Sprintf("%v", value) {
			return true
		}
	}
	return false
}

// parseStatusCode разбирает входные данные и возвращает нормализованное представление.
func parseStatusCode(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case float32:
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, err
		}
		return int(i), nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, err
		}
		return i, nil
	default:
		return 0, fmt.Errorf("unsupported status type %T", value)
	}
}

// toAnyMap выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toAnyMap(value any) (map[string]any, error) {
	switch v := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out, nil
	case json.RawMessage:
		var out map[string]any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("tool input must be an object")
		}
		var out map[string]any
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, fmt.Errorf("tool input must be an object")
		}
		return out, nil
	}
}

// valueAsString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func valueAsString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// valueAsStringMap выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func valueAsStringMap(value any) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	out := map[string]string{}
	switch v := value.(type) {
	case map[string]string:
		for key, item := range v {
			out[strings.TrimSpace(key)] = strings.TrimSpace(item)
		}
	case map[string]any:
		for key, item := range v {
			text := valueAsString(item)
			if text == "" && item != nil {
				return nil, fmt.Errorf("value for key %q must be string-like", key)
			}
			out[strings.TrimSpace(key)] = strings.TrimSpace(text)
		}
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("must be an object")
		}
		parsed := map[string]any{}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("must be an object")
		}
		for key, item := range parsed {
			text := valueAsString(item)
			if text == "" && item != nil {
				return nil, fmt.Errorf("value for key %q must be string-like", key)
			}
			out[strings.TrimSpace(key)] = strings.TrimSpace(text)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// findHeader выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func findHeader(headers map[string]string, name string) (string, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	for key, value := range headers {
		if strings.ToLower(strings.TrimSpace(key)) == target {
			return value, true
		}
	}
	return "", false
}

// copyStringMap возвращает независимую копию значения, чтобы избежать побочных мутаций.
func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// readResponseBodyLimited выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func readResponseBodyLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	limited := io.LimitReader(reader, maxBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds MAX_RESPONSE_BYTES=%d", maxBytes)
	}
	return payload, nil
}

// decodeResponseBody обновляет соответствующий счетчик или метрику наблюдаемости.
func decodeResponseBody(contentType string, payload []byte) responseBodyEnvelope {
	ct := strings.TrimSpace(contentType)
	if len(payload) == 0 {
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         "",
		}
	}

	if isJSONContentType(ct) || looksLikeJSONBytes(payload) {
		var parsed any
		if err := json.Unmarshal(payload, &parsed); err == nil {
			return responseBodyEnvelope{
				ContentType:  defaultResponseContentType(ct, "json"),
				BodyEncoding: "json",
				Body:         parsed,
			}
		}
	}

	if isUTF8Text(payload) {
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         string(payload),
		}
	}

	return responseBodyEnvelope{
		ContentType:  defaultResponseContentType(ct, "base64"),
		BodyEncoding: "base64",
		Body:         base64.StdEncoding.EncodeToString(payload),
	}
}

// bodyForValidation выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func bodyForValidation(value any) any {
	switch v := value.(type) {
	case responseBodyEnvelope:
		return bodyForValidation(v.Body)
	case map[string]any:
		if body, ok := extractBodyEnvelope(v); ok {
			return bodyForValidation(body.Body)
		}
	}

	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if looksLikeJSONString(trimmed) {
			var parsed any
			if err := json.Unmarshal([]byte(v), &parsed); err == nil {
				return parsed
			}
		}
		return v
	default:
		return v
	}
}

// normalizeResponseBody нормализует входные данные к канонической форме, используемой в модуле.
func normalizeResponseBody(contentType, bodyEncoding string, body any) responseBodyEnvelope {
	ct := strings.TrimSpace(contentType)
	encoding := strings.ToLower(strings.TrimSpace(bodyEncoding))

	if nested, ok := extractBodyEnvelopeMap(body); ok {
		if ct == "" {
			ct = nested.ContentType
		}
		if encoding == "" {
			encoding = nested.BodyEncoding
		}
		body = nested.Body
	}

	switch encoding {
	case "json":
		if s, ok := body.(string); ok {
			trimmed := strings.TrimSpace(s)
			if looksLikeJSONString(trimmed) {
				var parsed any
				if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
					body = parsed
				}
			}
		}
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "json"),
			BodyEncoding: "json",
			Body:         body,
		}
	case "text":
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         stringifyBody(body),
		}
	case "base64":
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "base64"),
			BodyEncoding: "base64",
			Body:         base64Body(body),
		}
	}

	switch value := body.(type) {
	case nil:
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         "",
		}
	case string:
		trimmed := strings.TrimSpace(value)
		if (isJSONContentType(ct) || looksLikeJSONString(trimmed)) && trimmed != "" {
			var parsed any
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return responseBodyEnvelope{
					ContentType:  defaultResponseContentType(ct, "json"),
					BodyEncoding: "json",
					Body:         parsed,
				}
			}
		}
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         value,
		}
	case []byte:
		return decodeResponseBody(ct, value)
	default:
		if isJSONObjectLike(value) {
			return responseBodyEnvelope{
				ContentType:  defaultResponseContentType(ct, "json"),
				BodyEncoding: "json",
				Body:         value,
			}
		}
		payload, err := json.Marshal(value)
		if err == nil {
			var parsed any
			if unmarshalErr := json.Unmarshal(payload, &parsed); unmarshalErr == nil {
				return responseBodyEnvelope{
					ContentType:  defaultResponseContentType(ct, "json"),
					BodyEncoding: "json",
					Body:         parsed,
				}
			}
		}
		return responseBodyEnvelope{
			ContentType:  defaultResponseContentType(ct, "text"),
			BodyEncoding: "text",
			Body:         stringifyBody(value),
		}
	}
}

// flattenHTTPHeaders преобразует вложенную структуру в плоский вид для сериализации/логирования.
func flattenHTTPHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[key] = strings.Join(values, ",")
	}
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sorted := make(map[string]string, len(out))
	for _, key := range keys {
		sorted[key] = out[key]
	}
	return sorted
}

// looksLikeJSONString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func looksLikeJSONString(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	return strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")
}

// looksLikeJSONBytes выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func looksLikeJSONBytes(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

// defaultResponseContentType выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func defaultResponseContentType(contentType, encoding string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType != "" {
		return contentType
	}
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "json":
		return "application/json"
	case "base64":
		return "application/octet-stream"
	default:
		return "text/plain; charset=utf-8"
	}
}

// isUTF8Text возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isUTF8Text(payload []byte) bool {
	if !utf8.Valid(payload) {
		return false
	}
	if len(payload) == 0 {
		return true
	}
	for _, b := range payload {
		if b == 0 {
			return false
		}
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			return false
		}
	}
	return true
}

// isJSONObjectLike возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isJSONObjectLike(value any) bool {
	switch value.(type) {
	case map[string]any, []any, bool, float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

// stringifyBody выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func stringifyBody(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		if isUTF8Text(v) {
			return string(v)
		}
		return base64.StdEncoding.EncodeToString(v)
	default:
		payload, err := json.Marshal(v)
		if err == nil {
			return string(payload)
		}
		return fmt.Sprintf("%v", v)
	}
}

// base64Body выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func base64Body(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	default:
		payload, err := json.Marshal(v)
		if err == nil {
			return base64.StdEncoding.EncodeToString(payload)
		}
		return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%v", v)))
	}
}

// extractBodyEnvelopeMap извлекает целевые данные из входного объекта с валидацией формата.
func extractBodyEnvelopeMap(value any) (responseBodyEnvelope, bool) {
	m, ok := value.(map[string]any)
	if !ok || len(m) == 0 {
		return responseBodyEnvelope{}, false
	}
	body, ok := extractBodyEnvelope(m)
	return body, ok
}

// extractBodyEnvelope извлекает целевые данные из входного объекта с валидацией формата.
func extractBodyEnvelope(m map[string]any) (responseBodyEnvelope, bool) {
	encoding := strings.ToLower(strings.TrimSpace(valueAsString(m["bodyEncoding"])))
	bodyValue, hasBody := m["body"]
	if encoding == "" || !hasBody {
		return responseBodyEnvelope{}, false
	}
	for key := range m {
		switch key {
		case "contentType", "bodyEncoding", "body":
		default:
			return responseBodyEnvelope{}, false
		}
	}
	return responseBodyEnvelope{
		ContentType:  strings.TrimSpace(valueAsString(m["contentType"])),
		BodyEncoding: encoding,
		Body:         bodyValue,
	}, true
}

// isJSONContentType возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(contentType, "json")
}

// uniqueStrings выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
