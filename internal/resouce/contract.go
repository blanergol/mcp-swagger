package resource

import (
	"context"
	"errors"
)

// ErrNotFound означает, что ресурс не найден.
var ErrNotFound = errors.New("resource not found")

// Descriptor описывает MCP-ресурс или шаблон URI ресурса.
type Descriptor struct {
	ID          string
	Name        string
	Description string
	URI         string
	URITemplate string
	MIMEType    string
}

// IsTemplate возвращает true, если дескриптор описывает шаблон URI.
func (d Descriptor) IsTemplate() bool {
	return d.URITemplate != ""
}

// Item хранит содержимое ресурса вместе с его дескриптором.
type Item struct {
	Descriptor Descriptor
	Text       string
}

// Store задает контракт чтения MCP-ресурсов.
type Store interface {
	List(ctx context.Context) ([]Descriptor, error)
	Get(ctx context.Context, id string) (Item, error)
}
