package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPerPrincipalConcurrencyIndependent проверяет ожидаемое поведение в тестовом сценарии.
func TestPerPrincipalConcurrencyIndependent(t *testing.T) {
	t.Parallel()

	transport := newControlledTransport()
	client := New(Options{
		Timeout:                 2 * time.Second,
		MaxConcurrent:           2,
		MaxConcurrentPerKey:     1,
		MaxCallsPerMinute:       120,
		MaxCallsPerMinutePerKey: 120,
	})
	client.httpClient.Transport = transport

	reqA, err := http.NewRequestWithContext(WithLimiterKey(context.Background(), "alice"), http.MethodGet, "https://api.example.com/a", nil)
	if err != nil {
		t.Fatalf("new request A: %v", err)
	}
	reqB, err := http.NewRequestWithContext(WithLimiterKey(context.Background(), "bob"), http.MethodGet, "https://api.example.com/b", nil)
	if err != nil {
		t.Fatalf("new request B: %v", err)
	}

	errCh := make(chan error, 2)
	go func() {
		resp, callErr := client.Do(reqA)
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		errCh <- callErr
	}()
	go func() {
		resp, callErr := client.Do(reqB)
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		errCh <- callErr
	}()

	keys := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case key := <-transport.started:
			keys[key] = true
		case <-time.After(300 * time.Millisecond):
			t.Fatalf("expected both requests to start without cross-principal blocking, got started keys: %#v", keys)
		}
	}
	if !keys["alice"] || !keys["bob"] {
		t.Fatalf("expected both principals to start concurrently, got keys: %#v", keys)
	}

	transport.releaseAll()

	for i := 0; i < 2; i++ {
		select {
		case callErr := <-errCh:
			if callErr != nil {
				t.Fatalf("unexpected Do error: %v", callErr)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for request completion")
		}
	}

	if transport.maxInFlight() < 2 {
		t.Fatalf("expected max in-flight >= 2, got %d", transport.maxInFlight())
	}
}

// TestPerPrincipalRateLimitExceededDoesNotBlockOtherPrincipal проверяет ожидаемое поведение в тестовом сценарии.
func TestPerPrincipalRateLimitExceededDoesNotBlockOtherPrincipal(t *testing.T) {
	t.Parallel()

	transport := newImmediateTransport()
	client := New(Options{
		Timeout:                 2 * time.Second,
		MaxConcurrent:           10,
		MaxConcurrentPerKey:     10,
		MaxCallsPerMinute:       100,
		MaxCallsPerMinutePerKey: 1,
	})
	client.httpClient.Transport = transport

	firstReq, err := http.NewRequestWithContext(WithLimiterKey(context.Background(), "alice"), http.MethodGet, "https://api.example.com/first", nil)
	if err != nil {
		t.Fatalf("new first request: %v", err)
	}
	firstResp, err := client.Do(firstReq)
	if err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	_ = firstResp.Body.Close()

	timeoutCtx, cancel := context.WithTimeout(WithLimiterKey(context.Background(), "alice"), 50*time.Millisecond)
	defer cancel()
	secondReq, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, "https://api.example.com/second", nil)
	if err != nil {
		t.Fatalf("new second request: %v", err)
	}
	_, err = client.Do(secondReq)
	if err == nil {
		t.Fatalf("second request for same principal should fail due per-principal rate limit")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded for rate-limited request, got: %v", err)
	}

	otherReq, err := http.NewRequestWithContext(WithLimiterKey(context.Background(), "bob"), http.MethodGet, "https://api.example.com/other", nil)
	if err != nil {
		t.Fatalf("new other request: %v", err)
	}
	otherResp, err := client.Do(otherReq)
	if err != nil {
		t.Fatalf("other principal request should not be blocked by alice quota: %v", err)
	}
	_ = otherResp.Body.Close()
}

// controlledTransport задает вспомогательную тестовую реализацию для изоляции сценария.
type controlledTransport struct {
	started chan string
	release chan struct{}

	mu          sync.Mutex
	inFlight    int
	maxObserved int
}

// newControlledTransport инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newControlledTransport() *controlledTransport {
	return &controlledTransport{
		started: make(chan string, 16),
		release: make(chan struct{}),
	}
}

// RoundTrip выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (t *controlledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	key := LimiterKeyFromContext(req.Context())
	t.started <- key

	t.mu.Lock()
	t.inFlight++
	if t.inFlight > t.maxObserved {
		t.maxObserved = t.inFlight
	}
	t.mu.Unlock()

	select {
	case <-req.Context().Done():
		t.mu.Lock()
		t.inFlight--
		t.mu.Unlock()
		return nil, req.Context().Err()
	case <-t.release:
	}

	t.mu.Lock()
	t.inFlight--
	t.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// releaseAll выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (t *controlledTransport) releaseAll() {
	close(t.release)
}

// maxInFlight выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (t *controlledTransport) maxInFlight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.maxObserved
}

// immediateTransport задает вспомогательную тестовую реализацию для изоляции сценария.
type immediateTransport struct{}

// newImmediateTransport инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newImmediateTransport() *immediateTransport {
	return &immediateTransport{}
}

// RoundTrip выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (t *immediateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
