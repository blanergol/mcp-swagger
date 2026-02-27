// Package auth содержит inbound-аутентификацию MCP HTTP транспорта.
//
// Пакет проверяет Bearer-токены (JWKS или introspection), формирует Principal,
// и предоставляет middleware, который кладет результат проверки в context.
package auth
