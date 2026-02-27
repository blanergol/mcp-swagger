package confirmation

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound означает, что подтверждение с указанным ID не найдено.
	ErrNotFound = errors.New("confirmation not found")
	// ErrExpired означает, что срок жизни подтверждения истек.
	ErrExpired = errors.New("confirmation expired")
	// ErrNotApproved означает, что подтверждение существует, но еще не одобрено.
	ErrNotApproved = errors.New("confirmation not approved")
	// ErrConsumed означает, что подтверждение уже использовано.
	ErrConsumed = errors.New("confirmation already consumed")
	// ErrMismatch означает, что подтверждение не соответствует текущему запросу.
	ErrMismatch = errors.New("confirmation request mismatch")
)

// Request описывает данные, необходимые для создания записи подтверждения.
type Request struct {
	OperationID            string
	Method                 string
	FinalURL               string
	PreparedRequestSummary any
	Reason                 string
}

// Check описывает параметры запроса, которые должны совпасть с одобренным подтверждением.
type Check struct {
	OperationID string
	Method      string
	FinalURL    string
}

// Record хранит текущее состояние подтверждения в рамках его жизненного цикла.
type Record struct {
	ID                     string
	OperationID            string
	Method                 string
	FinalURL               string
	PreparedRequestSummary any
	Reason                 string
	Approved               bool
	Consumed               bool
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

// Store задает контракт хранилища подтверждений.
type Store interface {
	Request(ctx context.Context, req Request) (Record, error)
	Confirm(ctx context.Context, id string, approve bool) (Record, error)
	ConsumeApproved(ctx context.Context, id string, check Check) (Record, error)
}
