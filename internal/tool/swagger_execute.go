package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/blanergol/mcp-swagger/internal/audit"
	authn "github.com/blanergol/mcp-swagger/internal/auth"
	"github.com/blanergol/mcp-swagger/internal/confirmation"
	"github.com/blanergol/mcp-swagger/internal/correlation"
	"github.com/blanergol/mcp-swagger/internal/httpclient"
	"github.com/blanergol/mcp-swagger/internal/metrics"
	"github.com/blanergol/mcp-swagger/internal/policy"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/upstreamauth"
)

// SwaggerExecuteOptions настраивает execute tool behavior.
type SwaggerExecuteOptions struct {
	Mode                   string
	UpstreamBaseURL        string
	UpstreamSandboxBaseURL string
	ValidateRequest        bool
	ValidateResponse       bool
	MaxRequestBytes        int64
	MaxResponseBytes       int64
	UserAgent              string
	CorrelationIDHeader    string
	ValidateURL            func(ctx context.Context, rawURL string) error
}

// SwaggerExecuteDependencies содержит execute tool dependencies.
type SwaggerExecuteDependencies struct {
	Store         swagger.Store
	Policy        policy.Evaluator
	AuthProvider  upstreamauth.Provider
	HTTPDoer      httpclient.Doer
	Auditor       audit.Logger
	Metrics       metrics.Recorder
	Confirmations confirmation.Store
	Options       SwaggerExecuteOptions
}

// SwaggerExecuteTool выполняет real HTTP calls through policy/auth/limits.
type SwaggerExecuteTool struct {
	store         swagger.Store
	policy        policy.Evaluator
	authProvider  upstreamauth.Provider
	httpDoer      httpclient.Doer
	auditor       audit.Logger
	metrics       metrics.Recorder
	confirmations confirmation.Store
	options       SwaggerExecuteOptions
}

// NewSwaggerExecuteTool создает swagger.http.execute tool.
func NewSwaggerExecuteTool(deps SwaggerExecuteDependencies) *SwaggerExecuteTool {
	policyEvaluator := deps.Policy
	if policyEvaluator == nil {
		policyEvaluator = policy.NewEvaluator(policy.Config{Mode: policy.ModePlanOnly})
	}
	authProvider := deps.AuthProvider
	if authProvider == nil {
		authProvider = upstreamauth.NewNoopProvider()
	}
	auditor := deps.Auditor
	if auditor == nil {
		auditor = audit.NewLogger(false, nil, nil)
	}
	metricsRecorder := deps.Metrics
	if metricsRecorder == nil {
		metricsRecorder = metrics.NewNoopRecorder()
	}
	return &SwaggerExecuteTool{
		store:         deps.Store,
		policy:        policyEvaluator,
		authProvider:  authProvider,
		httpDoer:      deps.HTTPDoer,
		auditor:       auditor,
		metrics:       metricsRecorder,
		confirmations: deps.Confirmations,
		options:       deps.Options,
	}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerExecuteTool) Name() string {
	return "swagger.http.execute"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerExecuteTool) Description() string {
	return "Executes a real upstream HTTP call through MCP policy, auth, limits and audit"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerExecuteTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerExecuteTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerExecuteTool) Execute(ctx context.Context, input any) (any, error) {
	startedAt := time.Now()
	t.metrics.IncExecuteInflight()
	defer t.metrics.DecExecuteInflight()
	defer func() {
		t.metrics.ObserveExecuteDuration(time.Since(startedAt).Seconds())
	}()

	auditEntry := audit.Entry{Timestamp: startedAt}
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		auditEntry.Principal = principal.Subject
		auditEntry.Scopes = append([]string(nil), principal.Scopes...)
	}

	fail := func(code, message string, details any) (any, error) {
		t.metrics.IncExecuteError(code)
		auditEntry.DurationMs = time.Since(startedAt).Milliseconds()
		auditEntry.Error = code + ": " + message
		_ = t.auditor.LogCall(ctx, auditEntry)
		return errorResult(code, message, details), nil
	}

	if t.store == nil {
		return fail("invalid_request", "swagger store is not configured", nil)
	}
	if t.httpDoer == nil {
		return fail("upstream_error", "HTTP client is not configured", nil)
	}

	reqInput, err := parseExecuteInput(input)
	if err != nil {
		return fail("invalid_request", err.Error(), nil)
	}

	endpoint, err := t.store.GetEndpointByOperationID(ctx, reqInput.OperationID)
	if err != nil {
		return fail("invalid_request", "operationId is not found in swagger", map[string]any{"operationId": reqInput.OperationID})
	}

	baseURL, err := t.resolveBaseURL(endpoint)
	if err != nil {
		return fail("no_base_url", err.Error(), map[string]any{"operationId": reqInput.OperationID})
	}
	if t.options.ValidateURL != nil {
		if err := t.options.ValidateURL(ctx, baseURL); err != nil {
			return fail("policy_denied", "base URL blocked by security policy", map[string]any{
				"operationId": reqInput.OperationID,
				"baseURL":     baseURL,
				"error":       err.Error(),
			})
		}
	}

	reqInput.BaseURL = baseURL
	prepared, err := prepareEndpointRequest(endpoint, reqInput, prepareOptions{
		BaseURL:       baseURL,
		AllowNoBase:   false,
		MaxBodyBytes:  t.options.MaxRequestBytes,
		DefaultAgent:  t.options.UserAgent,
		DefaultAccept: "application/json",
	})
	if err != nil {
		if err.Error() == "no base URL" {
			return fail("no_base_url", err.Error(), map[string]any{"operationId": reqInput.OperationID})
		}
		return fail("invalid_request", err.Error(), nil)
	}
	correlationHeader := correlation.HeaderName(t.options.CorrelationIDHeader)
	correlationHeader = textproto.CanonicalMIMEHeaderKey(correlationHeader)
	correlationID := strings.TrimSpace(firstHeaderValue(prepared.Headers, correlationHeader))
	if correlationID == "" {
		if ctxID, ok := correlation.IDFromContext(ctx); ok {
			correlationID = ctxID
		} else {
			correlationID = correlation.Generate()
		}
		if prepared.Headers == nil {
			prepared.Headers = map[string]string{}
		}
		prepared.Headers[correlationHeader] = correlationID
	}

	auditEntry.OperationID = prepared.OperationID
	auditEntry.Method = prepared.Method
	auditEntry.URL = prepared.URL
	auditEntry.CorrelationID = correlationID
	auditEntry.RequestHeaders = copyStringMap(prepared.Headers)
	auditEntry.RequestBody = prepared.BodyInput

	if t.options.ValidateURL != nil {
		if err := t.options.ValidateURL(ctx, prepared.URL); err != nil {
			return fail("policy_denied", "request URL blocked by security policy", map[string]any{
				"operationId": prepared.OperationID,
				"method":      prepared.Method,
				"finalURL":    prepared.URL,
				"error":       err.Error(),
			})
		}
	}

	decision, err := t.policy.Evaluate(ctx, prepared.OperationID, prepared.Method, prepared.URL)
	if err != nil {
		return fail("policy_denied", err.Error(), nil)
	}
	if !decision.Allow {
		code := strings.TrimSpace(decision.Code)
		if code == "" {
			code = "policy_denied"
		}
		details := map[string]any{
			"operationId": prepared.OperationID,
			"method":      prepared.Method,
			"url":         prepared.URL,
			"reason":      decision.Reason,
		}
		if decision.RequireConfirmation || code == "confirmation_required" {
			if t.confirmations == nil {
				return fail("confirmation_required", decision.Reason, map[string]any{
					"operationId":        prepared.OperationID,
					"method":             prepared.Method,
					"finalURL":           prepared.URL,
					"reason":             decision.Reason,
					"recommended_action": "call policy.request_confirmation then policy.confirm then retry swagger.http.execute with confirmationId",
				})
			}

			confirmationID := strings.TrimSpace(reqInput.ConfirmationID)
			if confirmationID == "" {
				return fail("confirmation_required", decision.Reason, map[string]any{
					"operationId":        prepared.OperationID,
					"method":             prepared.Method,
					"finalURL":           prepared.URL,
					"reason":             decision.Reason,
					"recommended_action": "call policy.request_confirmation then policy.confirm then retry swagger.http.execute with confirmationId",
					"next_tool":          "policy.request_confirmation",
					"suggested_input": map[string]any{
						"operationId": prepared.OperationID,
						"reason":      decision.Reason,
						"preparedRequestSummary": map[string]any{
							"operationId": prepared.OperationID,
							"method":      prepared.Method,
							"finalURL":    prepared.URL,
							"headers":     prepared.Headers,
							"queryParams": prepared.QueryParams,
							"pathParams":  prepared.PathParams,
						},
					},
				})
			}

			_, confirmErr := t.confirmations.ConsumeApproved(ctx, confirmationID, confirmation.Check{
				OperationID: prepared.OperationID,
				Method:      prepared.Method,
				FinalURL:    prepared.URL,
			})
			if confirmErr != nil {
				return fail("confirmation_required", "confirmation is missing, not approved, expired, already used, or mismatched", map[string]any{
					"operationId":        prepared.OperationID,
					"method":             prepared.Method,
					"finalURL":           prepared.URL,
					"confirmationId":     confirmationID,
					"reason":             decision.Reason,
					"error":              confirmErr.Error(),
					"recommended_action": "request and approve a fresh confirmation via policy.request_confirmation + policy.confirm",
				})
			}
		} else if code == "plan_only" {
			details["recommended_action"] = "switch MCP_API_MODE to execute_readonly|execute_write|sandbox"
			return fail(code, decision.Reason, details)
		} else {
			return fail(code, decision.Reason, details)
		}
	}

	if t.options.ValidateRequest {
		validationIssues := validatePreparedRequest(endpoint, prepared)
		if len(validationIssues) > 0 {
			return fail("invalid_request", "request does not match swagger schema", map[string]any{"errors": validationIssues})
		}
	}

	limiterKey := "anonymous"
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		subject := strings.TrimSpace(principal.Subject)
		if subject != "" {
			limiterKey = subject
		}
	}
	httpCtx := httpclient.WithLimiterKey(ctx, limiterKey)
	httpReq, err := http.NewRequestWithContext(httpCtx, prepared.Method, prepared.URL, bytes.NewReader(prepared.BodyBytes))
	if err != nil {
		return fail("invalid_request", err.Error(), nil)
	}
	for key, value := range prepared.Headers {
		httpReq.Header.Set(key, value)
	}
	if len(prepared.BodyBytes) > 0 {
		httpReq.ContentLength = int64(len(prepared.BodyBytes))
	}

	if err := t.authProvider.Apply(httpReq); err != nil {
		return fail("upstream_error", "failed to apply upstream auth", map[string]any{"error": err.Error()})
	}

	resp, err := t.httpDoer.Do(httpReq)
	if err != nil {
		if errors.Is(err, httpclient.ErrURLBlocked) {
			return fail("policy_denied", "request URL blocked by security policy", map[string]any{
				"operationId": prepared.OperationID,
				"method":      prepared.Method,
				"finalURL":    prepared.URL,
				"error":       err.Error(),
			})
		}
		if errors.Is(err, httpclient.ErrRequestTooLarge) {
			return fail("invalid_request", err.Error(), nil)
		}
		if errors.Is(err, httpclient.ErrRateLimited) {
			t.metrics.IncRateLimited()
			return fail("rate_limited", "request exceeded configured rate limit", map[string]any{
				"operationId": prepared.OperationID,
				"method":      prepared.Method,
				"finalURL":    prepared.URL,
			})
		}
		if isTimeoutError(err) {
			return fail("timeout", err.Error(), nil)
		}
		return fail("network_error", err.Error(), nil)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	auditEntry.ResponseStatus = resp.StatusCode
	auditEntry.ResponseHeaders = flattenHTTPHeaders(resp.Header)

	maxResponseBytes := t.options.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = 2 << 20
	}
	bodyPayload, truncated, err := readResponseBodyLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return fail("upstream_error", err.Error(), nil)
	}
	decoded := decodeResponseBody(resp.Header.Get("Content-Type"), bodyPayload)
	auditEntry.ResponseBody = decoded

	result := map[string]any{
		"operationId":  prepared.OperationID,
		"method":       prepared.Method,
		"finalURL":     prepared.URL,
		"status":       resp.StatusCode,
		"headers":      flattenHTTPHeaders(resp.Header),
		"contentType":  decoded.ContentType,
		"bodyEncoding": decoded.BodyEncoding,
		"body":         decoded.Body,
		"durationMs":   time.Since(startedAt).Milliseconds(),
	}
	if truncated {
		result["responseTruncated"] = true
		result["warnings"] = []string{fmt.Sprintf("response body exceeded MAX_RESPONSE_BYTES=%d and was truncated", maxResponseBytes)}
	}
	t.metrics.IncExecuteTotal(prepared.OperationID, prepared.Method, resp.StatusCode)

	if t.options.ValidateResponse {
		// Расхождение ответа с контрактом не ломает execute: ошибка уходит в responseValidation.
		validationIssues := validateResponse(endpoint, resp.StatusCode, bodyForValidation(decoded.Body))
		result["responseValidation"] = map[string]any{
			"valid":  len(validationIssues) == 0,
			"errors": validationIssues,
		}
	}

	auditEntry.DurationMs = time.Since(startedAt).Milliseconds()
	_ = t.auditor.LogCall(ctx, auditEntry)
	return okResult(result), nil
}

// resolveBaseURL вычисляет производное значение на основе входных данных и текущего состояния.
func (t *SwaggerExecuteTool) resolveBaseURL(endpoint swagger.ResolvedOperation) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(t.options.Mode))
	if mode == policy.ModeSandbox {
		base := strings.TrimSpace(t.options.UpstreamSandboxBaseURL)
		if base == "" {
			return "", errors.New("sandbox mode requires UPSTREAM_SANDBOX_BASE_URL")
		}
		return base, nil
	}

	if base := strings.TrimSpace(t.options.UpstreamBaseURL); base != "" {
		return base, nil
	}

	if len(endpoint.Servers) > 0 {
		server := strings.TrimSpace(endpoint.Servers[0])
		if server != "" {
			return server, nil
		}
	}

	if base := strings.TrimSpace(endpoint.BaseURL); base != "" {
		return base, nil
	}

	rawURL := strings.TrimSpace(endpoint.URLTemplate)
	if rawURL == "" {
		return "", errors.New("no base URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("no base URL")
	}
	base := parsed.Scheme + "://" + parsed.Host
	if parsed.Path != "" {
		if strings.HasSuffix(parsed.Path, endpoint.PathTemplate) {
			basePath := strings.TrimSuffix(parsed.Path, endpoint.PathTemplate)
			basePath = strings.TrimSuffix(basePath, "/")
			if basePath != "" {
				base += basePath
			}
		}
	}
	return base, nil
}

// isTimeoutError возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// firstHeaderValue возвращает первое подходящее значение по приоритетному правилу выбора.
func firstHeaderValue(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for key, value := range headers {
		if strings.ToLower(strings.TrimSpace(key)) == target {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
