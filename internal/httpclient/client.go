package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrRequestTooLarge обозначает request payload exceeded configured limit.
	ErrRequestTooLarge = errors.New("request body exceeds configured limit")
	// ErrURLBlocked обозначает request URL or redirect target was rejected by guardrails.
	ErrURLBlocked = errors.New("request url blocked by policy")
	// ErrRateLimited обозначает request was throttled by configured rate limit.
	ErrRateLimited = errors.New("request rate limited")
)

// anonymousLimiterKey используется для stdio/неаутентифицированных вызовов,
// когда principal.subject отсутствует в context.
const anonymousLimiterKey = "anonymous"

// limiterKeyContextKey — приватный ключ context для значения per-principal limiter key.
type limiterKeyContextKey struct{}

// WithLimiterKey сохраняет ключ субъекта для per-principal лимитеров в context.
func WithLimiterKey(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value := strings.TrimSpace(key)
	if value == "" {
		value = anonymousLimiterKey
	}
	return context.WithValue(ctx, limiterKeyContextKey{}, value)
}

// LimiterKeyFromContext извлекает ключ субъекта для per-principal лимитеров.
func LimiterKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return anonymousLimiterKey
	}
	value, _ := ctx.Value(limiterKeyContextKey{}).(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return anonymousLimiterKey
	}
	return value
}

// Doer выполняет outbound HTTP requests.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options настраивает outbound HTTP client wrappers.
type Options struct {
	Timeout                 time.Duration
	MaxConcurrent           int
	MaxCallsPerMinute       int
	MaxConcurrentPerKey     int
	MaxCallsPerMinutePerKey int
	MaxRequestBytes         int64
	ValidateURL             func(ctx context.Context, rawURL string) error
}

// Client является outbound HTTP doer with policy controls.
type Client struct {
	httpClient *http.Client
	semaphore  chan struct{}
	limiter    *minuteLimiter

	perKeySem     *keyedSemaphore
	perKeyLimiter *keyedMinuteLimiter

	maxRequestBytes int64
	validateURL     func(ctx context.Context, rawURL string) error
}

// New создает a new constrained HTTP client.
func New(opts Options) *Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	maxCallsPerMinute := opts.MaxCallsPerMinute
	if maxCallsPerMinute <= 0 {
		maxCallsPerMinute = 60
	}
	maxCallsPerMinutePerKey := opts.MaxCallsPerMinutePerKey
	if maxCallsPerMinutePerKey <= 0 {
		maxCallsPerMinutePerKey = maxCallsPerMinute
	}

	maxConcurrentPerKey := opts.MaxConcurrentPerKey
	if maxConcurrentPerKey <= 0 {
		maxConcurrentPerKey = maxConcurrent
	}

	maxRequestBytes := opts.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = 1 << 20
	}

	return &Client{
		httpClient:      buildHTTPClient(timeout, opts.ValidateURL),
		semaphore:       make(chan struct{}, maxConcurrent),
		limiter:         newMinuteLimiter(maxCallsPerMinute),
		perKeySem:       newKeyedSemaphore(maxConcurrentPerKey),
		perKeyLimiter:   newKeyedMinuteLimiter(maxCallsPerMinutePerKey),
		maxRequestBytes: maxRequestBytes,
		validateURL:     opts.ValidateURL,
	}
}

// Do выполняет request with concurrency/rate/size проверяет.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}

	if c.maxRequestBytes > 0 && req.ContentLength > c.maxRequestBytes {
		return nil, ErrRequestTooLarge
	}
	if c.validateURL != nil {
		if err := c.validateURL(req.Context(), req.URL.String()); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrURLBlocked, err)
		}
	}

	key := LimiterKeyFromContext(req.Context())
	release, err := c.acquire(req.Context(), key)
	if err != nil {
		return nil, err
	}
	defer release()

	if err := c.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	if err := c.perKeyLimiter.Wait(req.Context(), key); err != nil {
		return nil, err
	}

	return c.httpClient.Do(req)
}

// buildHTTPClient создает базовый http.Client без retry.
// Редиректы разрешаются только если validateURL пропущен, иначе каждый hop валидируется.
func buildHTTPClient(timeout time.Duration, validateURL func(ctx context.Context, rawURL string) error) *http.Client {
	client := &http.Client{Timeout: timeout}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if validateURL == nil {
			// Безопасный дефолт: без target-policy редиректы автоматически не выполняются.
			return http.ErrUseLastResponse
		}
		if err := validateURL(req.Context(), req.URL.String()); err != nil {
			return fmt.Errorf("%w: %v", ErrURLBlocked, err)
		}
		return nil
	}
	return client
}

// acquire резервирует глобальный и per-key слоты конкурентности.
// При ошибке захвата per-key слота глобальный слот освобождается, чтобы не было утечки емкости.
func (c *Client) acquire(ctx context.Context, key string) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.semaphore <- struct{}{}:
	}

	released := false
	release := func() {
		if released {
			return
		}
		released = true
		c.release()
	}

	if err := c.perKeySem.Acquire(ctx, key); err != nil {
		release()
		return nil, err
	}
	return func() {
		c.perKeySem.Release(key)
		release()
	}, nil
}

// release освобождает глобальный слот конкурентности.
func (c *Client) release() {
	select {
	case <-c.semaphore:
	default:
	}
}

// minuteLimiter реализует простой fixed-window лимитер на одну минуту.
type minuteLimiter struct {
	max int

	mu          sync.Mutex
	windowStart time.Time
	count       int
}

// newMinuteLimiter создает fixed-window лимитер с безопасным значением по умолчанию.
func newMinuteLimiter(limit int) *minuteLimiter {
	if limit <= 0 {
		limit = 60
	}
	return &minuteLimiter{max: limit}
}

// Wait ожидает свободный слот в текущем минутном окне.
// При отмене context возвращает ErrRateLimited, обернутый причиной отмены.
func (l *minuteLimiter) Wait(ctx context.Context) error {
	for {
		now := time.Now()

		l.mu.Lock()
		if l.windowStart.IsZero() || now.Sub(l.windowStart) >= time.Minute {
			l.windowStart = now
			l.count = 0
		}
		if l.count < l.max {
			l.count++
			l.mu.Unlock()
			return nil
		}
		waitFor := l.windowStart.Add(time.Minute).Sub(now)
		l.mu.Unlock()

		if waitFor <= 0 {
			continue
		}

		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("%w: %w", ErrRateLimited, ctx.Err())
		case <-timer.C:
		}
	}
}

// keyedMinuteLimiter хранит отдельный minuteLimiter на каждый ключ (principal).
type keyedMinuteLimiter struct {
	max int

	mu       sync.Mutex
	limiters map[string]*minuteLimiter
}

// newKeyedMinuteLimiter создает контейнер per-key лимитеров с общим максимальным значением.
func newKeyedMinuteLimiter(limit int) *keyedMinuteLimiter {
	if limit <= 0 {
		limit = 60
	}
	return &keyedMinuteLimiter{
		max:      limit,
		limiters: make(map[string]*minuteLimiter),
	}
}

// Wait применяет rate limit для конкретного ключа.
func (l *keyedMinuteLimiter) Wait(ctx context.Context, key string) error {
	if l == nil {
		return nil
	}
	key = normalizeLimiterKey(key)

	limiter := l.get(key)
	return limiter.Wait(ctx)
}

// get возвращает существующий per-key лимитер или лениво создает новый.
func (l *keyedMinuteLimiter) get(key string) *minuteLimiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiters[key]
	if !ok {
		limiter = newMinuteLimiter(l.max)
		l.limiters[key] = limiter
	}
	return limiter
}

// keyedSemaphore ограничивает конкурентность отдельно по каждому ключу.
type keyedSemaphore struct {
	max int

	mu     sync.Mutex
	values map[string]chan struct{}
}

// newKeyedSemaphore создает контейнер семафоров per-key с общей емкостью на ключ.
func newKeyedSemaphore(limit int) *keyedSemaphore {
	if limit <= 0 {
		limit = 10
	}
	return &keyedSemaphore{
		max:    limit,
		values: make(map[string]chan struct{}),
	}
}

// Acquire захватывает per-key слот конкурентности или завершает ожидание по context.
func (s *keyedSemaphore) Acquire(ctx context.Context, key string) error {
	if s == nil {
		return nil
	}
	sem := s.get(normalizeLimiterKey(key))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case sem <- struct{}{}:
		return nil
	}
}

// Release освобождает ранее занятый per-key слот конкурентности.
func (s *keyedSemaphore) Release(key string) {
	if s == nil {
		return
	}
	sem := s.get(normalizeLimiterKey(key))
	select {
	case <-sem:
	default:
	}
}

// get возвращает семафор для ключа, создавая его при первом обращении.
func (s *keyedSemaphore) get(key string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	sem, ok := s.values[key]
	if !ok {
		sem = make(chan struct{}, s.max)
		s.values[key] = sem
	}
	return sem
}

// normalizeLimiterKey приводит ключ к каноническому виду и подставляет anonymous fallback.
func normalizeLimiterKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return anonymousLimiterKey
	}
	return key
}
