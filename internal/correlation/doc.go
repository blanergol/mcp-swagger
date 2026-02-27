// Package correlation управляет correlation-id в context и HTTP-заголовках.
//
// Пакет используется для сквозной трассировки: идентификатор создается на входе,
// прокидывается в upstream-запрос и записывается в audit-логи.
package correlation
