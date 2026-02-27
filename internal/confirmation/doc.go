// Package confirmation реализует in-memory подтверждения для human-in-the-loop flow.
//
// Пакет хранит confirmation-записи с TTL и атомарным consume, чтобы write-вызовы
// можно было выполнить только после явного подтверждения пользователя.
package confirmation
