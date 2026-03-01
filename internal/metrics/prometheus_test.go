package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPrometheusRecorderExposesAndUpdatesMetrics protects metrics contract for all recorder primitives.
func TestPrometheusRecorderExposesAndUpdatesMetrics(t *testing.T) {
	t.Parallel()

	r := NewPrometheusRecorder()
	r.IncExecuteInflight()
	r.IncExecuteTotal("getUser", "get", 200)
	r.IncExecuteError("timeout")
	r.ObserveExecuteDuration(0.42)
	r.IncRateLimited()
	r.DecExecuteInflight()

	ts := httptest.NewServer(r.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status=%d want %d", resp.StatusCode, http.StatusOK)
	}
	payloadBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	payload := string(payloadBytes)

	required := []string{
		"mcp_execute_total",
		"mcp_execute_errors_total",
		"mcp_execute_duration_seconds",
		"mcp_execute_inflight",
		"mcp_rate_limited_total",
		`operationId="getUser"`,
		`method="GET"`,
		`status="200"`,
		`code="timeout"`,
	}
	for _, fragment := range required {
		if !strings.Contains(payload, fragment) {
			t.Fatalf("metrics payload missing fragment %q", fragment)
		}
	}
}

// TestPrometheusRecorderNilReceiverNoPanic protects nil-receiver guard contract.
func TestPrometheusRecorderNilReceiverNoPanic(t *testing.T) {
	t.Parallel()

	var r *PrometheusRecorder
	r.IncExecuteTotal("op", "GET", 200)
	r.IncExecuteError("x")
	r.ObserveExecuteDuration(1.0)
	r.IncExecuteInflight()
	r.DecExecuteInflight()
	r.IncRateLimited()

	h := r.Handler()
	if h == nil {
		t.Fatalf("nil receiver Handler must return non-nil handler")
	}
}

// TestNoopRecorderContract protects no-op recorder behavior and default handler response.
func TestNoopRecorderContract(t *testing.T) {
	t.Parallel()

	r := NewNoopRecorder()
	r.IncExecuteTotal("op", "GET", 200)
	r.IncExecuteError("code")
	r.ObserveExecuteDuration(1.2)
	r.IncExecuteInflight()
	r.DecExecuteInflight()
	r.IncRateLimited()

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("noop handler status=%d want %d", rec.Code, http.StatusNotFound)
	}
}

// TestNormalizeLabelValueMatrix protects label normalization behavior for empty/non-empty strings.
func TestNormalizeLabelValueMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "unknown"},
		{name: "spaces", in: "   ", want: "unknown"},
		{name: "already set", in: "getUser", want: "getUser"},
		{name: "trimmed", in: "  value  ", want: "value"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLabelValue(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeLabelValue(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}
