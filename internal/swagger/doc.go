// Package swagger загружает, парсит и индексирует OpenAPI/Swagger спецификацию.
//
// Пакет изолирует I/O (file/http loader), парсинг (json/yaml), резолвинг схем
// и кэширование документа, чтобы транспорт и usecase работали через интерфейсы Store/Loader.
package swagger
