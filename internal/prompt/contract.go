package prompt

import "errors"

// ErrNotFound означает, что шаблон prompt не найден.
var ErrNotFound = errors.New("prompt not found")

// Descriptor описывает доступный шаблон prompt.
type Descriptor struct {
	Name        string
	Description string
	Arguments   []string
}

// Store задает контракт хранилища prompt-шаблонов.
type Store interface {
	Get(name string) (template string, err error)
	List() []Descriptor
}
