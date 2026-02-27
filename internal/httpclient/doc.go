// Package httpclient предоставляет ограниченный HTTP-клиент для outbound вызовов.
//
// Пакет инкапсулирует timeout, лимиты размера, rate limit и concurrency limit
// (глобально и per-principal), а также проверку URL перед отправкой и редиректами.
package httpclient
