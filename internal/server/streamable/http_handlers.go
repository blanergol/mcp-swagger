package streamable

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/textproto"
	"strings"

	authn "github.com/blanergol/mcp-swagger/internal/auth"
	"github.com/blanergol/mcp-swagger/internal/correlation"
)

// newHTTPHandler инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func (s *Server) newHTTPHandler(mcpHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(s.handleHealthz))
	mux.Handle("/metrics", s.metricsHandler())

	protected := authn.Middleware(s.validator, s.withPrincipalLogging(http.MaxBytesHandler(mcpHandler, s.cfg.HTTPMaxBodyBytes)))
	mux.Handle("/mcp", protected)

	handler := s.withCorrelationID(mux)
	return s.withCORS(handler)
}

// metricsHandler выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *Server) metricsHandler() http.Handler {
	handler := http.NotFoundHandler()
	if s.metrics != nil {
		handler = s.metrics.Handler()
	}
	if s.cfg.MetricsAuthRequired {
		return authn.Middleware(s.validator, handler)
	}
	return handler
}

// handleHealthz выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": s.cfg.Version,
	})
}

// withPrincipalLogging оборачивает или дополняет поведение с учетом переданных параметров.
func (s *Server) withPrincipalLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := authn.PrincipalFromContext(r.Context()); ok {
			slog.InfoContext(r.Context(), "authenticated MCP request", "subject", principal.Subject, "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS оборачивает или дополняет поведение с учетом переданных параметров.
func (s *Server) withCORS(next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(s.cfg.CORSAllowedOrigins))
	allowAll := false
	for _, origin := range s.cfg.CORSAllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAll = true
			continue
		}
		allowedOrigins[origin] = struct{}{}
	}

	if len(allowedOrigins) == 0 && !allowAll {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		allowed := origin != "" && (allowAll || hasOrigin(allowedOrigins, origin))
		if allowed {
			appendVary(w.Header(), "Origin")
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "POST,GET,DELETE,OPTIONS")
			correlationHeader := correlation.HeaderName(s.cfg.CorrelationIDHeader)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Accept,Mcp-Session-Id,Last-Event-ID,"+correlationHeader)
		}

		if r.Method == http.MethodOptions {
			if !allowed {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// withCorrelationID оборачивает или дополняет поведение с учетом переданных параметров.
func (s *Server) withCorrelationID(next http.Handler) http.Handler {
	headerName := textproto.CanonicalMIMEHeaderKey(correlation.HeaderName(s.cfg.CorrelationIDHeader))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(headerName))
		if id == "" {
			id = correlation.Generate()
		}
		w.Header().Set(headerName, id)
		ctx := correlation.ContextWithID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// hasOrigin проверяет наличие требуемого значения в текущем контексте.
func hasOrigin(allow map[string]struct{}, origin string) bool {
	_, ok := allow[origin]
	return ok
}

// appendVary выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func appendVary(headers http.Header, value string) {
	current := headers.Get("Vary")
	if current == "" {
		headers.Set("Vary", value)
		return
	}
	for _, part := range strings.Split(current, ",") {
		if strings.EqualFold(strings.TrimSpace(part), value) {
			return
		}
	}
	headers.Set("Vary", current+", "+value)
}
