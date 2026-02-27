package policy

import (
	"context"
	"fmt"
	"strings"
)

const (
	// ModePlanOnly полностью запрещает реальное выполнение execute и оставляет только plan/validate.
	ModePlanOnly = "plan_only"
	// ModeExecuteReadonly разрешает только безопасные read-only методы.
	ModeExecuteReadonly = "execute_readonly"
	// ModeExecuteWrite разрешает write-вызовы при прохождении allow/deny правил.
	ModeExecuteWrite = "execute_write"
	// ModeSandbox логически эквивалентен write-режиму, но обычно используется с sandbox upstream.
	ModeSandbox = "sandbox"
)

// Config задает параметры политики для DefaultEvaluator.
type Config struct {
	Mode                        string
	AllowedMethods              []string
	DeniedMethods               []string
	AllowedOperationIDs         []string
	DeniedOperationIDs          []string
	RequireConfirmationForWrite bool
}

// DefaultEvaluator детерминированно применяет правила mode/method/operation.
type DefaultEvaluator struct {
	mode                        string
	allowedMethods              map[string]struct{}
	deniedMethods               map[string]struct{}
	allowedOperationIDs         map[string]struct{}
	deniedOperationIDs          map[string]struct{}
	requireConfirmationForWrite bool
}

// NewEvaluator создает evaluator с нормализацией входного конфигурационного среза.
func NewEvaluator(cfg Config) *DefaultEvaluator {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = ModePlanOnly
	}
	return &DefaultEvaluator{
		mode:                        mode,
		allowedMethods:              toUpperSet(cfg.AllowedMethods),
		deniedMethods:               toUpperSet(cfg.DeniedMethods),
		allowedOperationIDs:         toExactSet(cfg.AllowedOperationIDs),
		deniedOperationIDs:          toExactSet(cfg.DeniedOperationIDs),
		requireConfirmationForWrite: cfg.RequireConfirmationForWrite,
	}
}

// Evaluate проверяет policy для operation/method/url.
func (e *DefaultEvaluator) Evaluate(_ context.Context, opID, method, _ string) (Decision, error) {
	opID = strings.TrimSpace(opID)
	method = strings.ToUpper(strings.TrimSpace(method))

	// 1) Явный deny по operationId
	if opID != "" {
		if _, denied := e.deniedOperationIDs[opID]; denied {
			return deny("policy_denied", fmt.Sprintf("operationId %q is explicitly denied", opID)), nil
		}
	}

	// 2) Явный deny по HTTP-методу
	if method != "" {
		if _, denied := e.deniedMethods[method]; denied {
			return deny("policy_denied", fmt.Sprintf("HTTP method %q is explicitly denied", method)), nil
		}
	}

	// 3) allowlist operationId (если задан)
	if len(e.allowedOperationIDs) > 0 {
		if opID == "" {
			return deny("policy_denied", "operationId is required when ALLOWED_OPERATION_IDS is configured"), nil
		}
		if _, allowed := e.allowedOperationIDs[opID]; !allowed {
			return deny("policy_denied", fmt.Sprintf("operationId %q is not in allowlist", opID)), nil
		}
	}

	// 4) allowed methods (если задан)
	if len(e.allowedMethods) > 0 {
		if method == "" {
			return deny("policy_denied", "HTTP method is required when ALLOWED_METHODS is configured"), nil
		}
		if _, allowed := e.allowedMethods[method]; !allowed {
			return deny("policy_denied", fmt.Sprintf("HTTP method %q is not in allowlist", method)), nil
		}
	}

	// 5) Проверка режима MCP_API_MODE
	switch e.mode {
	case ModePlanOnly:
		return deny("plan_only", "execution is disabled in PLAN_ONLY mode"), nil
	case ModeExecuteReadonly:
		if !isReadonlyMethod(method) {
			return deny("policy_denied", fmt.Sprintf("HTTP method %q is not allowed in EXECUTE_READONLY mode", method)), nil
		}
		return allow(), nil
	case ModeExecuteWrite, ModeSandbox:
		// Режим write/sandbox пропускает к следующему шагу, если предыдущие проверки уже пройдены.
	default:
		return deny("plan_only", fmt.Sprintf("unsupported MCP_API_MODE %q", e.mode)), nil
	}

	// 6) confirmation_required для write-методов
	if e.requireConfirmationForWrite && isWriteMethod(method) {
		return Decision{
			Allow:               false,
			Code:                "confirmation_required",
			Reason:              fmt.Sprintf("method %q requires explicit user confirmation", method),
			RequireConfirmation: true,
		}, nil
	}

	return allow(), nil
}

// isReadonlyMethod определяет, относится ли метод к разрешенным read-only операциям.
func isReadonlyMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

// allow возвращает стандартное разрешающее решение policy.
func allow() Decision {
	return Decision{Allow: true}
}

// deny возвращает стандартное запрещающее решение с кодом и пояснением причины.
func deny(code, reason string) Decision {
	return Decision{Allow: false, Code: code, Reason: reason}
}

// toUpperSet нормализует список строк в upper-case множество без пустых значений.
func toUpperSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		item := strings.ToUpper(strings.TrimSpace(value))
		if item != "" {
			out[item] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// toExactSet строит множество строк без изменения регистра для точного сравнения operationId.
func toExactSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isWriteMethod определяет методы, которые считаются потенциально опасными write-операциями.
func isWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}
