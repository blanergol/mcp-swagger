package resource

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/tool"
)

const (
	// docsToolSchemasID фиксирует строковый маркер протокола/контракта, используемый в нескольких местах.
	docsToolSchemasID = "docs:tool-schemas"
	// docsToolSchemasURI фиксирует строковый маркер протокола/контракта, используемый в нескольких местах.
	docsToolSchemasURI = "docs://tool-schemas"
)

// DocsStore публикует статические ресурсы документации.
type DocsStore struct{}

// NewDocsStore создает resource.Store для документации.
func NewDocsStore() *DocsStore {
	return &DocsStore{}
}

// List возвращает список доступных ресурсов документации.
func (s *DocsStore) List(context.Context) ([]Descriptor, error) {
	return []Descriptor{docsToolSchemasDescriptor()}, nil
}

// Get возвращает содержимое документационного ресурса по ID или URI.
func (s *DocsStore) Get(_ context.Context, id string) (Item, error) {
	key := strings.TrimSpace(id)
	if key != docsToolSchemasID && key != docsToolSchemasURI {
		return Item{}, ErrNotFound
	}

	payload, err := json.MarshalIndent(tool.SchemasDocument(), "", "  ")
	if err != nil {
		return Item{}, err
	}
	return Item{
		Descriptor: docsToolSchemasDescriptor(),
		Text:       string(payload),
	}, nil
}

// docsToolSchemasDescriptor формирует дескриптор ресурса со схемами инструментов.
func docsToolSchemasDescriptor() Descriptor {
	return Descriptor{
		ID:          docsToolSchemasID,
		Name:        docsToolSchemasID,
		Description: "Formal JSON Schemas for MCP tool input/output contracts",
		URI:         docsToolSchemasURI,
		MIMEType:    "application/json",
	}
}
