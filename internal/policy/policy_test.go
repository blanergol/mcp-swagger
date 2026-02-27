package policy

import (
	"context"
	"strings"
	"testing"
)

// TestPriority1_DeniedOperationIDWinsOverEverything проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority1_DeniedOperationIDWinsOverEverything(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:                ModeExecuteWrite,
		AllowedMethods:      []string{"GET", "POST", "DELETE"},
		DeniedMethods:       []string{"DELETE"},
		AllowedOperationIDs: []string{"deleteUser"},
		DeniedOperationIDs:  []string{"deleteUser"},
	})

	decision := mustEval(t, evaluator, "deleteUser", "DELETE")
	if decision.Allow {
		t.Fatal("expected allow=false")
	}
	if decision.Code != "policy_denied" {
		t.Fatalf("unexpected code: %s", decision.Code)
	}
	if !strings.Contains(decision.Reason, "operationId") {
		t.Fatalf("expected operationId-first denial reason, got: %q", decision.Reason)
	}
}

// TestPriority2_DeniedMethodWinsAfterOperationCheck проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority2_DeniedMethodWinsAfterOperationCheck(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:                ModeExecuteWrite,
		AllowedMethods:      []string{"POST"},
		DeniedMethods:       []string{"POST"},
		AllowedOperationIDs: []string{"createUser"},
	})

	decision := mustEval(t, evaluator, "createUser", "POST")
	if decision.Allow {
		t.Fatal("expected allow=false")
	}
	if decision.Code != "policy_denied" {
		t.Fatalf("unexpected code: %s", decision.Code)
	}
	if !strings.Contains(decision.Reason, "HTTP method") {
		t.Fatalf("expected method denial reason, got: %q", decision.Reason)
	}
}

// TestPriority3_AllowlistOperationIDRestrictsWhenConfigured проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority3_AllowlistOperationIDRestrictsWhenConfigured(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:                ModeExecuteWrite,
		AllowedOperationIDs: []string{"getUser"},
		AllowedMethods:      []string{"GET"},
	})

	allowed := mustEval(t, evaluator, "getUser", "GET")
	if !allowed.Allow {
		t.Fatalf("expected allow=true for allowlisted operation, got: %+v", allowed)
	}

	denied := mustEval(t, evaluator, "createUser", "GET")
	if denied.Allow {
		t.Fatal("expected allow=false for non-allowlisted operation")
	}
	if denied.Code != "policy_denied" {
		t.Fatalf("unexpected code: %s", denied.Code)
	}
	if !strings.Contains(denied.Reason, "allowlist") {
		t.Fatalf("expected allowlist denial reason, got: %q", denied.Reason)
	}
}

// TestPriority4_AllowedMethodsRestrictWhenConfigured проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority4_AllowedMethodsRestrictWhenConfigured(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:           ModeExecuteWrite,
		AllowedMethods: []string{"GET"},
	})

	allowed := mustEval(t, evaluator, "getUser", "GET")
	if !allowed.Allow {
		t.Fatalf("expected allow=true for allowed method, got: %+v", allowed)
	}

	denied := mustEval(t, evaluator, "createUser", "POST")
	if denied.Allow {
		t.Fatal("expected allow=false for method outside allowlist")
	}
	if denied.Code != "policy_denied" {
		t.Fatalf("unexpected code: %s", denied.Code)
	}
	if !strings.Contains(denied.Reason, "allowlist") {
		t.Fatalf("expected allowed-methods denial reason, got: %q", denied.Reason)
	}
}

// TestPriority5_ModePlanOnlyDeniesAfterPassingLists проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority5_ModePlanOnlyDeniesAfterPassingLists(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:                ModePlanOnly,
		AllowedMethods:      []string{"GET"},
		AllowedOperationIDs: []string{"getUser"},
	})

	decision := mustEval(t, evaluator, "getUser", "GET")
	if decision.Allow {
		t.Fatal("expected allow=false in plan_only")
	}
	if decision.Code != "plan_only" {
		t.Fatalf("unexpected code: %s", decision.Code)
	}
}

// TestPriority5_ModeReadonlyDeniesWriteEvenIfMethodAllowlisted проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority5_ModeReadonlyDeniesWriteEvenIfMethodAllowlisted(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:           ModeExecuteReadonly,
		AllowedMethods: []string{"POST"},
	})

	decision := mustEval(t, evaluator, "createUser", "POST")
	if decision.Allow {
		t.Fatal("expected allow=false for write in readonly mode")
	}
	if decision.Code != "policy_denied" {
		t.Fatalf("unexpected code: %s", decision.Code)
	}
	if !strings.Contains(decision.Reason, "EXECUTE_READONLY") {
		t.Fatalf("expected readonly-mode denial reason, got: %q", decision.Reason)
	}
}

// TestPriority6_ConfirmationRequiredForWriteAfterModePass проверяет ожидаемое поведение в тестовом сценарии.
func TestPriority6_ConfirmationRequiredForWriteAfterModePass(t *testing.T) {
	evaluator := NewEvaluator(Config{
		Mode:                        ModeExecuteWrite,
		AllowedMethods:              []string{"POST"},
		RequireConfirmationForWrite: true,
	})

	decision := mustEval(t, evaluator, "createUser", "POST")
	if decision.Allow {
		t.Fatal("expected allow=false due to confirmation requirement")
	}
	if decision.Code != "confirmation_required" {
		t.Fatalf("unexpected code: %s", decision.Code)
	}
	if !decision.RequireConfirmation {
		t.Fatal("expected RequireConfirmation=true")
	}
}

// mustEval выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustEval(t *testing.T, evaluator *DefaultEvaluator, opID, method string) Decision {
	t.Helper()
	decision, err := evaluator.Evaluate(context.Background(), opID, method, "https://api.example.com/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return decision
}
