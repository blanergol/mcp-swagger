package streamable

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blanergol/mcp-swagger/config"
	authn "github.com/blanergol/mcp-swagger/internal/auth"
	"github.com/blanergol/mcp-swagger/internal/metrics"
	"github.com/blanergol/mcp-swagger/internal/prompt"
	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/tool"
	"github.com/blanergol/mcp-swagger/internal/usecase"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testProtocolVersion фиксирует константу контракта, используемую в нескольких точках пакета.
const testProtocolVersion = "2025-06-18"

// testValidator задает вспомогательную тестовую реализацию для изоляции сценария.
type testValidator struct{}

// Validate выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func (testValidator) Validate(_ context.Context, token string) (authn.Principal, error) {
	if strings.TrimSpace(token) != "valid-token" {
		return authn.Principal{}, &authn.UnauthorizedError{Reason: "invalid token"}
	}
	return authn.Principal{
		Subject: "itest-user",
		Scopes:  []string{"mcp:tools.call"},
	}, nil
}

// TestHealthzEndpoint проверяет ожидаемое поведение в тестовом сценарии.
func TestHealthzEndpoint(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("GET /healthz content-type = %q, want application/json", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	if got := payload["status"]; got != "ok" {
		t.Fatalf("/healthz status field = %#v, want %q", got, "ok")
	}
	if got := payload["version"]; got != "test-version" {
		t.Fatalf("/healthz version field = %#v, want %q", got, "test-version")
	}
	if correlationID := strings.TrimSpace(resp.Header.Get("X-Correlation-Id")); correlationID == "" {
		t.Fatalf("GET /healthz should include generated X-Correlation-Id header")
	}
}

// TestCorrelationIDMiddlewarePreservesProvidedHeader проверяет ожидаемое поведение в тестовом сценарии.
func TestCorrelationIDMiddlewarePreservesProvidedHeader(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Correlation-Id", "cid-test-123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("X-Correlation-Id"); got != "cid-test-123" {
		t.Fatalf("expected response correlation id to preserve client value, got %q", got)
	}
}

// TestMCPInitializeOverHTTP проверяет ожидаемое поведение в тестовом сценарии.
func TestMCPInitializeOverHTTP(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, body := mustInitializeSession(t, ts.URL)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("POST /mcp initialize status = %d, want 2xx, body=%s", resp.StatusCode, string(body))
	}
	if got := resp.Header.Get("Mcp-Session-Id"); strings.TrimSpace(got) == "" {
		t.Fatalf("POST /mcp initialize missing Mcp-Session-Id header")
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("POST /mcp content-type = %q, want text/event-stream", got)
	}

	payload := string(body)
	if !strings.Contains(payload, `"jsonrpc":"2.0"`) {
		t.Fatalf("initialize response does not contain jsonrpc envelope: %s", payload)
	}
	if !strings.Contains(payload, `"id":1`) {
		t.Fatalf("initialize response does not contain id=1: %s", payload)
	}
	if !strings.Contains(payload, `"result"`) {
		t.Fatalf("initialize response does not contain result: %s", payload)
	}
}

// TestMCPStreamableLifecycleAndListEndpoints проверяет ожидаемое поведение в тестовом сценарии.
func TestMCPStreamableLifecycleAndListEndpoints(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	initResp, _ := mustInitializeSession(t, ts.URL)
	sessionID := strings.TrimSpace(initResp.Header.Get("Mcp-Session-Id"))
	if sessionID == "" {
		t.Fatalf("initialize must return Mcp-Session-Id")
	}

	initializedPayload := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	initializedResp := mustMCPRequest(t, mcpRequest{
		BaseURL:   ts.URL,
		Method:    http.MethodPost,
		Token:     "valid-token",
		SessionID: sessionID,
		Accept:    "application/json, text/event-stream",
		Body:      initializedPayload,
	})
	initializedBody := readAllAndClose(t, initializedResp)
	if initializedResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST notifications/initialized status=%d want=%d body=%s", initializedResp.StatusCode, http.StatusAccepted, string(initializedBody))
	}

	toolsPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	toolsResp := mustMCPRequest(t, mcpRequest{
		BaseURL:   ts.URL,
		Method:    http.MethodPost,
		Token:     "valid-token",
		SessionID: sessionID,
		Accept:    "application/json, text/event-stream",
		Body:      toolsPayload,
	})
	toolsBody := readAllAndClose(t, toolsResp)
	if toolsResp.StatusCode != http.StatusOK {
		t.Fatalf("POST tools/list status=%d want=%d body=%s", toolsResp.StatusCode, http.StatusOK, string(toolsBody))
	}
	if got := toolsResp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("tools/list content-type=%q want text/event-stream", got)
	}

	toolsResult := mustFindJSONRPCResult(t, toolsBody, 2)
	tools, ok := toolsResult["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list result missing tools array: %#v", toolsResult)
	}

	resourcesPayload := `{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}`
	resourcesResp := mustMCPRequest(t, mcpRequest{
		BaseURL:   ts.URL,
		Method:    http.MethodPost,
		Token:     "valid-token",
		SessionID: sessionID,
		Accept:    "application/json, text/event-stream",
		Body:      resourcesPayload,
	})
	resourcesBody := readAllAndClose(t, resourcesResp)
	if resourcesResp.StatusCode != http.StatusOK {
		t.Fatalf("POST resources/list status=%d want=%d body=%s", resourcesResp.StatusCode, http.StatusOK, string(resourcesBody))
	}
	resourcesResult := mustFindJSONRPCResult(t, resourcesBody, 3)
	resources, ok := resourcesResult["resources"].([]any)
	if !ok || len(resources) == 0 {
		t.Fatalf("resources/list result missing resources array: %#v", resourcesResult)
	}
}

// TestMCPStandaloneGETAndDelete проверяет ожидаемое поведение в тестовом сценарии.
func TestMCPStandaloneGETAndDelete(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	initResp, _ := mustInitializeSession(t, ts.URL)
	sessionID := strings.TrimSpace(initResp.Header.Get("Mcp-Session-Id"))
	if sessionID == "" {
		t.Fatalf("initialize must return Mcp-Session-Id")
	}

	getResp := mustMCPRequest(t, mcpRequest{
		BaseURL:   ts.URL,
		Method:    http.MethodGet,
		Token:     "valid-token",
		SessionID: sessionID,
		Accept:    "text/event-stream",
	})
	if getResp.StatusCode != http.StatusOK {
		body := readAllAndClose(t, getResp)
		t.Fatalf("GET /mcp status=%d want=%d body=%s", getResp.StatusCode, http.StatusOK, string(body))
	}
	if got := getResp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("GET /mcp content-type=%q want text/event-stream", got)
	}

	readDone := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		buf := make([]byte, 1)
		n, err := getResp.Body.Read(buf)
		readDone <- struct {
			n   int
			err error
		}{n: n, err: err}
	}()

	select {
	case res := <-readDone:
		if res.err == io.EOF {
			t.Fatalf("GET /mcp stream closed unexpectedly (EOF)")
		}
	case <-time.After(200 * time.Millisecond):
		// Для висящего GET-stream допустимо отсутствие немедленных данных, но заголовки должны быть валидны.
	}
	_ = getResp.Body.Close()

	deleteResp := mustMCPRequest(t, mcpRequest{
		BaseURL:   ts.URL,
		Method:    http.MethodDelete,
		Token:     "valid-token",
		SessionID: sessionID,
		Accept:    "application/json, text/event-stream",
	})
	deleteBody := readAllAndClose(t, deleteResp)
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /mcp status=%d want=%d body=%s", deleteResp.StatusCode, http.StatusNoContent, string(deleteBody))
	}
}

// TestMCPHeaderValidation проверяет ожидаемое поведение в тестовом сценарии.
func TestMCPHeaderValidation(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	t.Run("post_without_authorization", func(t *testing.T) {
		resp := mustMCPRequest(t, mcpRequest{
			BaseURL: ts.URL,
			Method:  http.MethodPost,
			Accept:  "application/json, text/event-stream",
			Body:    initializePayload(1),
		})
		body := readAllAndClose(t, resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("POST /mcp without auth status=%d want=%d body=%s", resp.StatusCode, http.StatusUnauthorized, string(body))
		}
	})

	t.Run("post_without_event_stream_accept", func(t *testing.T) {
		resp := mustMCPRequest(t, mcpRequest{
			BaseURL: ts.URL,
			Method:  http.MethodPost,
			Token:   "valid-token",
			Accept:  "application/json",
			Body:    initializePayload(1),
		})
		body := readAllAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST /mcp without text/event-stream in Accept status=%d want=%d body=%s", resp.StatusCode, http.StatusBadRequest, string(body))
		}
	})

	t.Run("get_without_session_id", func(t *testing.T) {
		resp := mustMCPRequest(t, mcpRequest{
			BaseURL: ts.URL,
			Method:  http.MethodGet,
			Token:   "valid-token",
			Accept:  "text/event-stream",
		})
		body := readAllAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET /mcp without session id status=%d want=%d body=%s", resp.StatusCode, http.StatusBadRequest, string(body))
		}
	})

	t.Run("delete_without_session_id", func(t *testing.T) {
		resp := mustMCPRequest(t, mcpRequest{
			BaseURL: ts.URL,
			Method:  http.MethodDelete,
			Token:   "valid-token",
			Accept:  "application/json, text/event-stream",
		})
		body := readAllAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("DELETE /mcp without session id status=%d want=%d body=%s", resp.StatusCode, http.StatusBadRequest, string(body))
		}
	})
}

// TestMetricsEndpoint проверяет ожидаемое поведение в тестовом сценарии.
func TestMetricsEndpoint(t *testing.T) {
	handler := newTestHTTPHandler(t)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	body := readAllAndClose(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(body))
	}
	payload := string(body)
	if !strings.Contains(payload, "mcp_execute_total") {
		t.Fatalf("metrics payload must contain mcp_execute_total, got: %s", payload)
	}
	if !strings.Contains(payload, "mcp_execute_duration_seconds_bucket") {
		t.Fatalf("metrics payload must contain mcp_execute_duration_seconds_bucket, got: %s", payload)
	}
}

// TestMetricsEndpointAuthRequired проверяет ожидаемое поведение в тестовом сценарии.
func TestMetricsEndpointAuthRequired(t *testing.T) {
	cfg := config.Config{
		Version:             "test-version",
		HTTPMaxBodyBytes:    1 << 20,
		HTTPSessionTimeout:  time.Minute,
		MetricsAuthRequired: true,
	}
	handler := newTestHTTPHandlerWithConfig(t, cfg)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	body := readAllAndClose(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /metrics without auth status=%d want=%d body=%s", resp.StatusCode, http.StatusUnauthorized, string(body))
	}

	authReq, err := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	if err != nil {
		t.Fatalf("new auth metrics request: %v", err)
	}
	authReq.Header.Set("Authorization", "Bearer valid-token")
	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		t.Fatalf("authorized GET /metrics failed: %v", err)
	}
	authBody := readAllAndClose(t, authResp)
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("authorized GET /metrics status=%d want=%d body=%s", authResp.StatusCode, http.StatusOK, string(authBody))
	}
}

// mcpRequest описывает служебную структуру данных для передачи между шагами обработки.
type mcpRequest struct {
	BaseURL   string
	Method    string
	Token     string
	SessionID string
	Accept    string
	Body      string
}

// mustInitializeSession выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustInitializeSession(t *testing.T, baseURL string) (*http.Response, []byte) {
	t.Helper()
	resp := mustMCPRequest(t, mcpRequest{
		BaseURL: baseURL,
		Method:  http.MethodPost,
		Token:   "valid-token",
		Accept:  "application/json, text/event-stream",
		Body:    initializePayload(1),
	})
	body := readAllAndClose(t, resp)
	return resp, body
}

// initializePayload выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func initializePayload(id int) string {
	return fmt.Sprintf(`{
  "jsonrpc":"2.0",
  "id":%d,
  "method":"initialize",
  "params":{
    "protocolVersion":"%s",
    "clientInfo":{"name":"itest-client","version":"0.0.1"},
    "capabilities":{}
  }
}`, id, testProtocolVersion)
}

// mustMCPRequest выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustMCPRequest(t *testing.T, reqCfg mcpRequest) *http.Response {
	t.Helper()

	var body io.Reader
	if reqCfg.Body != "" {
		body = bytes.NewBufferString(reqCfg.Body)
	}
	req, err := http.NewRequest(reqCfg.Method, reqCfg.BaseURL+"/mcp", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if reqCfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+reqCfg.Token)
	}
	if reqCfg.Accept != "" {
		req.Header.Set("Accept", reqCfg.Accept)
	}
	if reqCfg.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if reqCfg.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", reqCfg.SessionID)
	}
	req.Header.Set("Mcp-Protocol-Version", testProtocolVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s /mcp request failed: %v", reqCfg.Method, err)
	}
	return resp
}

// readAllAndClose выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func readAllAndClose(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		t.Fatalf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	return body
}

// mustFindJSONRPCResult выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustFindJSONRPCResult(t *testing.T, ssePayload []byte, wantID int) map[string]any {
	t.Helper()
	events := extractSSEDataEvents(t, ssePayload)
	if len(events) == 0 {
		t.Fatalf("no SSE data events found in payload: %q", string(ssePayload))
	}
	for _, event := range events {
		msg := map[string]any{}
		if err := json.Unmarshal(event, &msg); err != nil {
			continue
		}
		if strings.TrimSpace(valueAsString(msg["jsonrpc"])) != "2.0" {
			continue
		}
		if !jsonRPCIDEquals(msg["id"], wantID) {
			continue
		}
		result, ok := msg["result"].(map[string]any)
		if !ok {
			t.Fatalf("json-rpc response for id=%d has no result object: %#v", wantID, msg)
		}
		return result
	}
	t.Fatalf("json-rpc response with id=%d not found in SSE payload: %s", wantID, string(ssePayload))
	return nil
}

// extractSSEDataEvents извлекает целевые данные из входного объекта с валидацией формата.
func extractSSEDataEvents(t *testing.T, payload []byte) [][]byte {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 1024), 1<<20)

	events := make([][]byte, 0)
	lines := make([]string, 0)
	flush := func() {
		if len(lines) == 0 {
			return
		}
		events = append(events, []byte(strings.Join(lines, "\n")))
		lines = lines[:0]
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE payload: %v", err)
	}
	return events
}

// jsonRPCIDEquals выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func jsonRPCIDEquals(value any, want int) bool {
	switch v := value.(type) {
	case float64:
		return int(v) == want
	case int:
		return v == want
	case int64:
		return int(v) == want
	case json.Number:
		n, err := v.Int64()
		return err == nil && int(n) == want
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return err == nil && n == want
	default:
		return false
	}
}

// valueAsString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func valueAsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

// newTestHTTPHandler инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newTestHTTPHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		Version:            "test-version",
		HTTPMaxBodyBytes:   1 << 20,
		HTTPSessionTimeout: time.Minute,
	}
	return newTestHTTPHandlerWithConfig(t, cfg)
}

// newTestHTTPHandlerWithConfig инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newTestHTTPHandlerWithConfig(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()

	recorder := metrics.NewPrometheusRecorder()

	registry := tool.NewRegistry(
		tool.NewEchoTool(),
		tool.NewHealthTool(cfg.Version),
	)
	promptStore := prompt.NewMemoryStore()
	resourceStore := resource.NewMemoryStore()
	swaggerStore := swagger.NewNoopStore()
	svc := usecase.NewService(registry, promptStore, resourceStore, swaggerStore)

	srv := &Server{
		cfg:       cfg,
		usecase:   svc,
		registry:  registry,
		prompts:   promptStore,
		resources: resourceStore,
		validator: testValidator{},
		metrics:   recorder,
	}

	mcpServer, err := srv.buildMCPServer()
	if err != nil {
		t.Fatalf("build mcp server: %v", err)
	}
	streamableHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: cfg.HTTPSessionTimeout,
		Logger:         slog.Default(),
	})

	return srv.newHTTPHandler(streamableHandler)
}
