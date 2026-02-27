package metrics

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusRecorder реализует Recorder на выделенном prometheus.Registry.
type PrometheusRecorder struct {
	registry *prometheus.Registry

	executeTotal       *prometheus.CounterVec
	executeErrorsTotal *prometheus.CounterVec
	executeDuration    prometheus.Histogram
	executeInflight    prometheus.Gauge
	rateLimitedTotal   prometheus.Counter
}

// NewPrometheusRecorder создает регистратор с полным набором обязательных метрик.
func NewPrometheusRecorder() *PrometheusRecorder {
	registry := prometheus.NewRegistry()

	executeTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_execute_total",
		Help: "Total number of successful upstream execute calls.",
	}, []string{"operationId", "method", "status"})
	executeErrorsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_execute_errors_total",
		Help: "Total number of execute errors grouped by code.",
	}, []string{"code"})
	executeDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "mcp_execute_duration_seconds",
		Help:    "Duration of swagger.http.execute calls in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	executeInflight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_execute_inflight",
		Help: "Current number of in-flight swagger.http.execute calls.",
	})
	rateLimitedTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mcp_rate_limited_total",
		Help: "Total number of execute calls rejected by rate limiting.",
	})

	registry.MustRegister(
		executeTotal,
		executeErrorsTotal,
		executeDuration,
		executeInflight,
		rateLimitedTotal,
	)
	// Предсоздаем серии, чтобы HELP/TYPE и базовые time-series были доступны до первого execute-вызова.
	executeTotal.WithLabelValues("unknown", "UNKNOWN", "0").Add(0)
	executeErrorsTotal.WithLabelValues("unknown").Add(0)

	return &PrometheusRecorder{
		registry:           registry,
		executeTotal:       executeTotal,
		executeErrorsTotal: executeErrorsTotal,
		executeDuration:    executeDuration,
		executeInflight:    executeInflight,
		rateLimitedTotal:   rateLimitedTotal,
	}
}

// NewNoopRecorder создает без действия metrics recorder.
func NewNoopRecorder() Recorder {
	return noopRecorder{}
}

// IncExecuteTotal обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) IncExecuteTotal(operationID, method string, status int) {
	if r == nil {
		return
	}
	r.executeTotal.WithLabelValues(
		normalizeLabelValue(operationID),
		normalizeLabelValue(strings.ToUpper(strings.TrimSpace(method))),
		strconv.Itoa(status),
	).Inc()
}

// IncExecuteError обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) IncExecuteError(code string) {
	if r == nil {
		return
	}
	r.executeErrorsTotal.WithLabelValues(normalizeLabelValue(code)).Inc()
}

// ObserveExecuteDuration обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) ObserveExecuteDuration(seconds float64) {
	if r == nil {
		return
	}
	r.executeDuration.Observe(seconds)
}

// IncExecuteInflight обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) IncExecuteInflight() {
	if r == nil {
		return
	}
	r.executeInflight.Inc()
}

// DecExecuteInflight обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) DecExecuteInflight() {
	if r == nil {
		return
	}
	r.executeInflight.Dec()
}

// IncRateLimited обновляет соответствующий счетчик или метрику наблюдаемости.
func (r *PrometheusRecorder) IncRateLimited() {
	if r == nil {
		return
	}
	r.rateLimitedTotal.Inc()
}

// Handler возвращает HTTP handler для экспонирования текущей функциональности.
func (r *PrometheusRecorder) Handler() http.Handler {
	if r == nil || r.registry == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

// noopRecorder описывает внутренние данные, которые не должны утекать в публичный API пакета.
type noopRecorder struct{}

// IncExecuteTotal обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) IncExecuteTotal(string, string, int) {}

// IncExecuteError обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) IncExecuteError(string) {}

// ObserveExecuteDuration обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) ObserveExecuteDuration(float64) {}

// IncExecuteInflight обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) IncExecuteInflight() {}

// DecExecuteInflight обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) DecExecuteInflight() {}

// IncRateLimited обновляет соответствующий счетчик или метрику наблюдаемости.
func (noopRecorder) IncRateLimited() {}

// Handler возвращает HTTP handler для экспонирования текущей функциональности.
func (noopRecorder) Handler() http.Handler { return http.NotFoundHandler() }

// normalizeLabelValue нормализует входные данные к канонической форме, используемой в модуле.
func normalizeLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
