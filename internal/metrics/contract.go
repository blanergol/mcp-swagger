package metrics

import "net/http"

// Recorder задает интерфейс сбора метрик execute-потока и экспонирования HTTP-handler.
type Recorder interface {
	IncExecuteTotal(operationID, method string, status int)
	IncExecuteError(code string)
	ObserveExecuteDuration(seconds float64)
	IncExecuteInflight()
	DecExecuteInflight()
	IncRateLimited()
	Handler() http.Handler
}
