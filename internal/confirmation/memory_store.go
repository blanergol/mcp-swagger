package confirmation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// defaultTTL задает значение по умолчанию, используемое при пустой или неполной конфигурации.
const defaultTTL = 10 * time.Minute

// MemoryStore хранит подтверждения в памяти и автоматически истекает записи по TTL.
type MemoryStore struct {
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	items map[string]Record
}

// NewMemoryStore создает in-memory хранилище подтверждений.
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &MemoryStore{
		ttl:   ttl,
		now:   time.Now,
		items: make(map[string]Record),
	}
}

// Request создает новую запись подтверждения и возвращает ее снимок.
func (s *MemoryStore) Request(_ context.Context, req Request) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked("")
	id, err := newID()
	if err != nil {
		return Record{}, err
	}

	now := s.now().UTC()
	record := Record{
		ID:                     id,
		OperationID:            strings.TrimSpace(req.OperationID),
		Method:                 strings.ToUpper(strings.TrimSpace(req.Method)),
		FinalURL:               strings.TrimSpace(req.FinalURL),
		PreparedRequestSummary: req.PreparedRequestSummary,
		Reason:                 strings.TrimSpace(req.Reason),
		Approved:               false,
		Consumed:               false,
		CreatedAt:              now,
		ExpiresAt:              now.Add(s.ttl),
	}
	s.items[id] = record
	return cloneRecord(record), nil
}

// Confirm обновляет статус одобрения у существующего подтверждения.
func (s *MemoryStore) Confirm(_ context.Context, id string, approve bool) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	s.cleanupLocked(id)
	item, ok := s.items[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if isExpired(s.now(), item.ExpiresAt) {
		delete(s.items, item.ID)
		return Record{}, ErrExpired
	}
	if item.Consumed {
		return Record{}, ErrConsumed
	}
	item.Approved = approve
	s.items[item.ID] = item
	return cloneRecord(item), nil
}

// ConsumeApproved проверяет, что подтверждение одобрено и подходит запросу, после чего помечает его использованным.
func (s *MemoryStore) ConsumeApproved(_ context.Context, id string, check Check) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	s.cleanupLocked(id)
	item, ok := s.items[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if isExpired(s.now(), item.ExpiresAt) {
		delete(s.items, item.ID)
		return Record{}, ErrExpired
	}
	if item.Consumed {
		return Record{}, ErrConsumed
	}
	if !item.Approved {
		return Record{}, ErrNotApproved
	}

	if !matches(item.OperationID, check.OperationID) ||
		!matches(item.Method, strings.ToUpper(strings.TrimSpace(check.Method))) ||
		!matches(item.FinalURL, check.FinalURL) {
		return Record{}, ErrMismatch
	}

	item.Consumed = true
	s.items[item.ID] = item
	return cloneRecord(item), nil
}

// cleanupLocked выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *MemoryStore) cleanupLocked(skipID string) {
	now := s.now()
	for id, item := range s.items {
		if id == skipID {
			continue
		}
		if isExpired(now, item.ExpiresAt) {
			delete(s.items, id)
		}
	}
}

// isExpired возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isExpired(now, expiresAt time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	return !expiresAt.After(now)
}

// matches выполняет проверку соответствия по правилам текущего модуля.
func matches(expected, actual string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	return expected == strings.TrimSpace(actual)
}

// cloneRecord создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneRecord(record Record) Record {
	record.PreparedRequestSummary = cloneAny(record.PreparedRequestSummary)
	return record
}

// cloneAny создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneAny(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneAny(v[i])
		}
		return out
	default:
		return v
	}
}

// newID инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
