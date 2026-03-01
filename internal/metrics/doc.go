// Package metrics описывает метрики выполнения swagger.http.execute.
//
// Пакет предоставляет интерфейс Recorder и Prometheus-реализацию,
// чтобы слой tool мог собирать телеметрию без жесткой привязки к backend-метрик.
//
//revive:disable:var-naming
package metrics
