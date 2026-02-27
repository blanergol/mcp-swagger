// Package streamable реализует MCP Streamable HTTP transport.
//
// Пакет поднимает HTTP endpoints (/healthz, /mcp, /metrics), применяет middleware
// (auth, correlation-id, CORS, body limits) и делегирует JSON-RPC обработку SDK handler.
package streamable
