// Package upstreamauth реализует авторизацию MCP к реальному upstream API.
//
// Пакет поддерживает режимы none/static bearer/api key/oauth client credentials,
// включая кэширование access token и централизованное применение в HTTP-запросах.
package upstreamauth
