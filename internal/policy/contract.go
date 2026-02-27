package policy

import "context"

// Evaluator определяет, разрешен ли вызов upstream API.
type Evaluator interface {
	Evaluate(ctx context.Context, opID, method, targetURL string) (Decision, error)
}

// Decision описывает результат проверки guardrails-политики.
type Decision struct {
	Allow               bool   `json:"allow"`
	Reason              string `json:"reason,omitempty"`
	RequireConfirmation bool   `json:"requireConfirmation,omitempty"`
	Code                string `json:"code,omitempty"`
}
