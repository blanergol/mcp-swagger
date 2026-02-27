// Package netguard реализует SSRF-защиту для исходящих URL.
//
// Пакет проверяет allowlist хостов, блокирует private/loopback/link-local сети
// и используется как при старте, так и перед фактическими HTTP-вызовами.
package netguard
