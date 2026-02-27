package swagger

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultSwaggerHTTPTimeout ограничивает общее время HTTP-загрузки SWAGGER_PATH.
	DefaultSwaggerHTTPTimeout = 10 * time.Second
	// DefaultSwaggerMaxBytes ограничивает размер загружаемой спецификации для файла и URL.
	DefaultSwaggerMaxBytes int64 = 5 * 1024 * 1024
	// DefaultSwaggerUserAgent отправляется в HTTP-запросах загрузчика swagger.
	DefaultSwaggerUserAgent = "MCP-Swagger-Loader/1.0"
	// DefaultSwaggerMaxRedirects ограничивает число переходов по редиректам.
	DefaultSwaggerMaxRedirects = 5
)

// URLValidator проверяет URL источника и URL редиректов до выполнения запроса.
type URLValidator func(ctx context.Context, rawURL string) error

// SourceLoaderOption изменяет конфигурацию SourceLoader.
type SourceLoaderOption func(*sourceLoaderConfig)

// SourceLoader загружает OpenAPI из локального файла или HTTP(S) URL.
type SourceLoader struct {
	source string
	loader Loader
}

// sourceLoaderConfig описывает параметры выбора и настройки конкретного loader-а.
type sourceLoaderConfig struct {
	httpClient   *http.Client
	validate     URLValidator
	httpTimeout  time.Duration
	maxBytes     int64
	userAgent    string
	maxRedirects int
}

// sourceKind определяет, как именно нужно читать SWAGGER_PATH.
type sourceKind string

const (
	// sourceKindFile означает чтение спецификации из локального файла.
	sourceKindFile sourceKind = "file"
	// sourceKindHTTP означает загрузку спецификации по HTTP(S).
	sourceKindHTTP sourceKind = "http"
	// sourceKindUnsupportedURL используется для URL со схемой, отличной от http/https.
	sourceKindUnsupportedURL sourceKind = "unsupported_url"
)

// invalidLoader сохраняет ошибку выбора источника и возвращает ее при фактической загрузке.
type invalidLoader struct {
	err error
}

// Load загружает данные из источника с учетом ограничений и ошибок ввода/вывода.
func (l invalidLoader) Load(context.Context) ([]byte, error) {
	if l.err != nil {
		return nil, l.err
	}
	return nil, ErrUnavailable
}

// FileLoader загружает swagger-спецификацию из локального файла.
type FileLoader struct {
	path     string
	maxBytes int64
}

// FileLoaderOptions задает лимиты чтения для файлового загрузчика.
type FileLoaderOptions struct {
	MaxBytes int64
}

// HTTPLoader загружает swagger-спецификацию по HTTP(S).
type HTTPLoader struct {
	source       string
	httpClient   *http.Client
	validate     URLValidator
	maxBytes     int64
	userAgent    string
	maxRedirects int
}

// HTTPLoaderOptions задает ограничения и политику HTTP-загрузчика.
type HTTPLoaderOptions struct {
	HTTPClient   *http.Client
	Timeout      time.Duration
	MaxBytes     int64
	UserAgent    string
	MaxRedirects int
	URLValidator URLValidator
}

// WithURLValidator задает валидатор URL источника и редиректов.
func WithURLValidator(validator URLValidator) SourceLoaderOption {
	return func(cfg *sourceLoaderConfig) {
		cfg.validate = validator
	}
}

// WithHTTPTimeout переопределяет HTTP-таймаут загрузки swagger по URL.
func WithHTTPTimeout(timeout time.Duration) SourceLoaderOption {
	return func(cfg *sourceLoaderConfig) {
		if timeout > 0 {
			cfg.httpTimeout = timeout
		}
	}
}

// WithMaxBytes переопределяет лимит размера swagger для файлов и URL.
func WithMaxBytes(maxBytes int64) SourceLoaderOption {
	return func(cfg *sourceLoaderConfig) {
		if maxBytes > 0 {
			cfg.maxBytes = maxBytes
		}
	}
}

// WithUserAgent переопределяет User-Agent для URL-загрузки swagger.
func WithUserAgent(userAgent string) SourceLoaderOption {
	return func(cfg *sourceLoaderConfig) {
		agent := strings.TrimSpace(userAgent)
		if agent != "" {
			cfg.userAgent = agent
		}
	}
}

// WithMaxRedirects переопределяет лимит числа редиректов.
func WithMaxRedirects(maxRedirects int) SourceLoaderOption {
	return func(cfg *sourceLoaderConfig) {
		if maxRedirects > 0 {
			cfg.maxRedirects = maxRedirects
		}
	}
}

// NewSourceLoader создает загрузчик источника по пути или URL.
func NewSourceLoader(source string, httpClient *http.Client, opts ...SourceLoaderOption) *SourceLoader {
	cfg := sourceLoaderConfig{
		httpClient:   httpClient,
		httpTimeout:  DefaultSwaggerHTTPTimeout,
		maxBytes:     DefaultSwaggerMaxBytes,
		userAgent:    DefaultSwaggerUserAgent,
		maxRedirects: DefaultSwaggerMaxRedirects,
	}
	for _, option := range opts {
		if option != nil {
			option(&cfg)
		}
	}
	trimmedSource := strings.TrimSpace(source)
	delegate := newSourceDelegate(trimmedSource, cfg)
	loader := &SourceLoader{
		source: trimmedSource,
		loader: delegate,
	}
	return loader
}

// Load читает сырой swagger payload из выбранного источника.
func (l *SourceLoader) Load(ctx context.Context) ([]byte, error) {
	if l == nil || l.source == "" || l.loader == nil {
		return nil, ErrUnavailable
	}
	return l.loader.Load(ctx)
}

// NewFileLoader создает файловый загрузчик спецификации.
func NewFileLoader(path string, opts FileLoaderOptions) *FileLoader {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultSwaggerMaxBytes
	}
	return &FileLoader{
		path:     strings.TrimSpace(path),
		maxBytes: maxBytes,
	}
}

// Load читает swagger-спецификацию с локальной файловой системы.
func (l *FileLoader) Load(_ context.Context) ([]byte, error) {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return nil, ErrUnavailable
	}
	file, err := os.Open(l.path)
	if err != nil {
		return nil, fmt.Errorf("read swagger file: %w", err)
	}
	defer file.Close()

	if info, statErr := file.Stat(); statErr == nil && info.Size() > l.maxBytes {
		return nil, fmt.Errorf("swagger payload exceeds configured size limit (%d bytes)", l.maxBytes)
	}
	return readAllWithLimit(file, l.maxBytes)
}

// NewHTTPLoader создает HTTP(S)-загрузчик спецификации.
func NewHTTPLoader(source string, opts HTTPLoaderOptions) *HTTPLoader {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultSwaggerHTTPTimeout
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultSwaggerMaxBytes
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = DefaultSwaggerUserAgent
	}
	maxRedirects := opts.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = DefaultSwaggerMaxRedirects
	}

	client := cloneHTTPClient(opts.HTTPClient)
	if client.Timeout <= 0 {
		client.Timeout = timeout
	}

	loader := &HTTPLoader{
		source:       strings.TrimSpace(source),
		httpClient:   client,
		validate:     opts.URLValidator,
		maxBytes:     maxBytes,
		userAgent:    userAgent,
		maxRedirects: maxRedirects,
	}
	loader.configureRedirectPolicy()
	return loader
}

// Load получает swagger bytes over HTTP(S).
func (l *HTTPLoader) Load(ctx context.Context) ([]byte, error) {
	if l == nil || strings.TrimSpace(l.source) == "" {
		return nil, ErrUnavailable
	}
	parsed, err := url.Parse(l.source)
	if err != nil {
		return nil, fmt.Errorf("invalid swagger URL: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported swagger URL scheme %q: only http/https are allowed", parsed.Scheme)
	}

	if l.validate != nil {
		if err := l.validate(ctx, l.source); err != nil {
			return nil, fmt.Errorf("swagger url blocked by policy: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, text/plain, */*")
	if l.userAgent != "" {
		req.Header.Set("User-Agent", l.userAgent)
	}

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch swagger url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("swagger url returned status %d", resp.StatusCode)
	}

	if resp.ContentLength > 0 && resp.ContentLength > l.maxBytes {
		return nil, fmt.Errorf("swagger payload exceeds configured size limit (%d bytes)", l.maxBytes)
	}
	payload, err := readAllWithLimit(resp.Body, l.maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read swagger response: %w", err)
	}
	return payload, nil
}

// detectSourceKind определяет тип источника и блокирует неподдерживаемые URL-схемы.
func detectSourceKind(source string) (sourceKind, *url.URL, error) {
	parsed, isURL := parseURLSource(source)
	if !isURL {
		return sourceKindFile, nil, nil
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "http", "https":
		return sourceKindHTTP, parsed, nil
	default:
		return sourceKindUnsupportedURL, parsed, fmt.Errorf("unsupported swagger URL scheme %q: only http/https are allowed", parsed.Scheme)
	}
}

// parseURLSource пытается распознать строку как URL-источник.
// Для локальных путей без схемы возвращает false.
func parseURLSource(source string) (*url.URL, bool) {
	raw := strings.TrimSpace(source)
	if raw == "" {
		return nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" {
		return nil, false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	prefix := scheme + "://"
	if strings.HasPrefix(strings.ToLower(raw), prefix) || scheme == "file" {
		return parsed, true
	}
	return nil, false
}

// newSourceDelegate выбирает конкретную реализацию Loader (file/http)
// и применяет общие ограничения размера/timeout/redirect-policy.
func newSourceDelegate(source string, cfg sourceLoaderConfig) Loader {
	if strings.TrimSpace(source) == "" {
		return invalidLoader{err: ErrUnavailable}
	}
	kind, _, err := detectSourceKind(source)
	if err != nil {
		return invalidLoader{err: err}
	}
	switch kind {
	case sourceKindHTTP:
		return NewHTTPLoader(source, HTTPLoaderOptions{
			HTTPClient:   cfg.httpClient,
			Timeout:      cfg.httpTimeout,
			MaxBytes:     cfg.maxBytes,
			UserAgent:    cfg.userAgent,
			MaxRedirects: cfg.maxRedirects,
			URLValidator: cfg.validate,
		})
	case sourceKindFile:
		return NewFileLoader(source, FileLoaderOptions{MaxBytes: cfg.maxBytes})
	default:
		return invalidLoader{err: fmt.Errorf("unsupported swagger source kind: %s", kind)}
	}
}

// cloneHTTPClient создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneHTTPClient(source *http.Client) *http.Client {
	if source == nil {
		return &http.Client{Timeout: DefaultSwaggerHTTPTimeout}
	}
	clone := *source
	return &clone
}

// configureRedirectPolicy ограничивает redirect-цепочку и валидирует каждый target URL.
// Это предотвращает обход allowlist через промежуточные редиректы.
func (l *HTTPLoader) configureRedirectPolicy() {
	if l.httpClient == nil {
		l.httpClient = &http.Client{Timeout: DefaultSwaggerHTTPTimeout}
	}
	existing := l.httpClient.CheckRedirect
	l.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= l.maxRedirects {
			return fmt.Errorf("too many redirects while fetching swagger (max %d)", l.maxRedirects)
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("invalid redirect target")
		}
		scheme := strings.ToLower(strings.TrimSpace(req.URL.Scheme))
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("swagger redirect blocked: unsupported scheme %q", req.URL.Scheme)
		}
		if l.validate != nil {
			if err := l.validate(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("swagger redirect blocked by policy: %w", err)
			}
		}
		if existing != nil {
			return existing(req, via)
		}
		return nil
	}
}

// readAllWithLimit читает payload с hard-limit по размеру.
// Ограничение применяется и для file, и для HTTP загрузки.
func readAllWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultSwaggerMaxBytes
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("swagger payload exceeds configured size limit (%d bytes)", maxBytes)
	}
	return payload, nil
}
