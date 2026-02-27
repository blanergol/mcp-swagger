package tool

import (
	"encoding/json"
	"testing"
)

// TestParseExecuteInputNewFormat проверяет ожидаемое поведение в тестовом сценарии.
func TestParseExecuteInputNewFormat(t *testing.T) {
	raw := json.RawMessage(`{
		"operationId":"getUserById",
		"params":{
			"path":{"id":"123"},
			"query":{"verbose":true,"limit":10},
			"headers":{"X-Correlation-Id":"req-001"},
			"body":{"include":"profile"}
		},
		"contentType":"application/json",
		"baseURL":"https://api.example.com",
		"confirmationId":"confirm-123"
	}`)

	parsed, err := parseExecuteInput(raw)
	if err != nil {
		t.Fatalf("parseExecuteInput(new): %v", err)
	}
	if parsed.OperationID != "getUserById" {
		t.Fatalf("unexpected operationId: %q", parsed.OperationID)
	}
	if parsed.PathParams["id"] != "123" {
		t.Fatalf("unexpected path param id: %q", parsed.PathParams["id"])
	}
	if parsed.QueryParams["verbose"] != "true" {
		t.Fatalf("unexpected query verbose: %q", parsed.QueryParams["verbose"])
	}
	if parsed.QueryParams["limit"] != "10" {
		t.Fatalf("unexpected query limit: %q", parsed.QueryParams["limit"])
	}
	if parsed.Headers["X-Correlation-Id"] != "req-001" {
		t.Fatalf("unexpected header: %q", parsed.Headers["X-Correlation-Id"])
	}
	if parsed.ContentType != "application/json" {
		t.Fatalf("unexpected contentType: %q", parsed.ContentType)
	}
	if parsed.BaseURL != "https://api.example.com" {
		t.Fatalf("unexpected baseURL: %q", parsed.BaseURL)
	}
	if parsed.ConfirmationID != "confirm-123" {
		t.Fatalf("unexpected confirmationId: %q", parsed.ConfirmationID)
	}
}

// TestParseExecuteInputLegacyFormat проверяет ожидаемое поведение в тестовом сценарии.
func TestParseExecuteInputLegacyFormat(t *testing.T) {
	raw := json.RawMessage(`{
		"operationId":"getUserById",
		"pathParams":{"id":"123"},
		"queryParams":{"verbose":"true"},
		"headers":{"X-Correlation-Id":"req-legacy"},
		"body":{"include":"profile"},
		"contentType":"application/json",
		"baseURL":"https://legacy.example.com",
		"confirmationToken":"legacy-token"
	}`)

	parsed, err := parseExecuteInput(raw)
	if err != nil {
		t.Fatalf("parseExecuteInput(legacy): %v", err)
	}
	if parsed.PathParams["id"] != "123" {
		t.Fatalf("unexpected path param id: %q", parsed.PathParams["id"])
	}
	if parsed.QueryParams["verbose"] != "true" {
		t.Fatalf("unexpected query verbose: %q", parsed.QueryParams["verbose"])
	}
	if parsed.Headers["X-Correlation-Id"] != "req-legacy" {
		t.Fatalf("unexpected header: %q", parsed.Headers["X-Correlation-Id"])
	}
	if parsed.BaseURL != "https://legacy.example.com" {
		t.Fatalf("unexpected baseURL: %q", parsed.BaseURL)
	}
	if parsed.ConfirmationID != "legacy-token" {
		t.Fatalf("unexpected confirmation token mapping: %q", parsed.ConfirmationID)
	}
}

// TestParseSearchInputNewAndLegacy проверяет ожидаемое поведение в тестовом сценарии.
func TestParseSearchInputNewAndLegacy(t *testing.T) {
	newRaw := json.RawMessage(`{
		"params":{
			"query":{"query":"find user","method":"get","tag":"users","schema":"User","status":404,"include":["endpoints","usage"],"limit":5}
		}
	}`)
	parsedNew, err := parseSearchInput(newRaw)
	if err != nil {
		t.Fatalf("parseSearchInput(new): %v", err)
	}
	if parsedNew.Query != "find user" || parsedNew.Method != "GET" || parsedNew.Tag != "users" || parsedNew.Limit != 5 {
		t.Fatalf("unexpected parsed new search input: %+v", parsedNew)
	}
	if parsedNew.Schema != "User" || parsedNew.Status != 404 {
		t.Fatalf("unexpected schema/status in parsed new search input: %+v", parsedNew)
	}
	if len(parsedNew.Include) != 2 {
		t.Fatalf("unexpected include in parsed new search input: %+v", parsedNew.Include)
	}

	legacyRaw := json.RawMessage(`{"query":"find order","method":"post","tag":"orders","limit":3}`)
	parsedLegacy, err := parseSearchInput(legacyRaw)
	if err != nil {
		t.Fatalf("parseSearchInput(legacy): %v", err)
	}
	if parsedLegacy.Query != "find order" || parsedLegacy.Method != "POST" || parsedLegacy.Tag != "orders" || parsedLegacy.Limit != 3 {
		t.Fatalf("unexpected parsed legacy search input: %+v", parsedLegacy)
	}
	if len(parsedLegacy.Include) != 1 || parsedLegacy.Include[0] != "endpoints" {
		t.Fatalf("expected default include=[endpoints], got %+v", parsedLegacy.Include)
	}
}

// TestParseValidateResponseInputNewAndLegacy проверяет ожидаемое поведение в тестовом сценарии.
func TestParseValidateResponseInputNewAndLegacy(t *testing.T) {
	newRaw := json.RawMessage(`{
		"operationId":"getUserById",
		"params":{
			"query":{"status":200},
			"headers":{"Content-Type":"application/json"},
			"body":{"id":"123"}
		}
	}`)
	parsedNew, err := parseValidateResponseInput(newRaw)
	if err != nil {
		t.Fatalf("parseValidateResponseInput(new): %v", err)
	}
	if parsedNew.OperationID != "getUserById" || parsedNew.Status != 200 {
		t.Fatalf("unexpected parsed new input: %+v", parsedNew)
	}
	if parsedNew.Headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected parsed new headers: %+v", parsedNew.Headers)
	}

	legacyRaw := json.RawMessage(`{
		"operationId":"getUserById",
		"status":404,
		"headers":{"X-Request-Id":"abc"},
		"body":{"error":"not found"}
	}`)
	parsedLegacy, err := parseValidateResponseInput(legacyRaw)
	if err != nil {
		t.Fatalf("parseValidateResponseInput(legacy): %v", err)
	}
	if parsedLegacy.Status != 404 {
		t.Fatalf("unexpected parsed legacy status: %d", parsedLegacy.Status)
	}
	if parsedLegacy.Headers["X-Request-Id"] != "abc" {
		t.Fatalf("unexpected parsed legacy headers: %+v", parsedLegacy.Headers)
	}
}
