# MCP Swagger Gateway (Go)

> [English](README.md) | **Русский**

MCP-сервер на Go, который:

- загружает Swagger/OpenAPI (JSON/YAML, файл или URL),
- публикует структуру API как MCP resources,
- даёт инструменты MCP для подготовки/валидации/безопасного выполнения реальных HTTP-вызовов,
- работает как контролируемый gateway между AI-агентом и upstream API.

## Quick Start

1. Подготовка:

```bash
make tidy
make build
```

2. Запуск в `stdio` (по умолчанию):

```bash
export TRANSPORT=stdio
export SWAGGER_PATH=./openapi.yaml
export MCP_API_MODE=plan_only
make run
```

Тот же запуск через CLI-аргументы:

```bash
go run ./cmd/mcp-server \
  --transport=stdio \
  --swagger-path=./openapi.yaml \
  --mcp-api-mode=plan_only
```

3. Запуск с `SWAGGER_PATH` как URL (production-safe):

```bash
export TRANSPORT=stdio
export SWAGGER_PATH=https://specs.example.com/openapi.yaml
export SWAGGER_ALLOWED_HOSTS=specs.example.com
export BLOCK_PRIVATE_NETWORKS=true
export SWAGGER_HTTP_TIMEOUT=10s
export SWAGGER_MAX_BYTES=5242880
export SWAGGER_USER_AGENT=MCP-Swagger-Loader/1.0
make run
```

4. Запуск `streamable` HTTP + inbound OAuth (пример JWKS):

```bash
export TRANSPORT=streamable
export HTTP_ADDR=:8080
export SWAGGER_PATH=./openapi.yaml

export INBOUND_OAUTH_ISSUER=https://issuer.example.com
export INBOUND_OAUTH_AUDIENCE=mcp-api
export INBOUND_OAUTH_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
export INBOUND_OAUTH_REQUIRED_SCOPES=mcp:tools.call

make run
```

5. Проверка:

```bash
curl -s http://127.0.0.1:8080/healthz
```

### Docker Compose

1. Скопируйте пример env-файла и заполните значения:

```bash
cp .env.example .env
# отредактируйте .env — задайте SWAGGER_PATH, INBOUND_OAUTH_JWKS_URL и т.д.
```

2. Соберите и запустите:

```bash
docker compose up --build -d
```

3. Убедитесь, что сервис работает:

```bash
curl -s http://127.0.0.1:8080/healthz
```

4. Просмотр логов / остановка:

```bash
docker compose logs -f
docker compose down
```

Контейнер использует транспорт `streamable` на порту `8080` по умолчанию.
Переопределить хост-порт можно через `HTTP_PORT` в `.env`.
Вся конфигурация передаётся через переменные окружения — полный список см. в `.env.example`.

---

## Статус реализации (аудит по коду)

| Функция | Статус | Комментарий |
|---|---|---|
| MCP transport `stdio` | ✅ реализовано | `internal/server/stdio` |
| MCP transport `streamable HTTP` | ✅ реализовано | `/healthz` + `/mcp` через SDK handler |
| Детали HTTP-поведения `/mcp` (Accept/session/version) | ⚠️ ограничение/условие | Поведение частично определяется `go-sdk` (`mcp.NewStreamableHTTPHandler`) |
| Inbound OAuth 2.1 (JWKS + Introspection) | ✅ реализовано | `internal/auth` |
| Outbound auth к upstream (none/static/api_key/oauth_cc) | ✅ реализовано | `internal/upstreamauth` |
| Guardrails/policy (mode + allow/deny + confirmation_required) | ✅ реализовано | `internal/policy` |
| Human-in-the-loop confirmation flow (`policy.request_confirmation`, `policy.confirm`) | ✅ реализовано | In-memory TTL store + проверка в `swagger.http.execute` |
| SSRF-защита (allowlist + private networks + redirect policy) | ✅ реализовано | `internal/netguard`, проверки на старте и перед execute |
| Swagger resources (`swagger:endpoints/...`) | ✅ реализовано | `internal/resouce/swagger_store.go` |
| Формальные JSON Schema для tools + docs resource | ✅ реализовано | `internal/tool/schemas.go` + resource `docs:tool-schemas` |
| Swagger schema resolution (`$ref`) | ✅ реализовано | `$ref` раскрываются рекурсивно |
| Композиции `allOf/oneOf/anyOf` | ⚠️ ограничение/условие | Структуры сохраняются как есть, вложенные `$ref` раскрываются, полного merge нет |
| Циклические схемы | ⚠️ ограничение/условие | Маркеры `x-circularRef`/`x-unresolvedRef` вместо бесконечного раскрытия |
| Structured audit logging + redaction | ✅ реализовано | `internal/audit` |
| Автогенерация correlation id + прокидка в upstream/audit | ✅ реализовано | middleware streamable + fallback в `swagger.http.execute` |
| Встроенные метрики Prometheus (`/metrics`) | ✅ реализовано | `internal/metrics` + endpoint `GET /metrics` |

## Table of Contents

- [Docker Compose](#docker-compose)
- [Статус реализации (аудит по коду)](#статус-реализации-аудит-по-коду)
- [Что это и зачем](#что-это-и-зачем)
- [Архитектура](#архитектура)
- [Транспорты MCP](#транспорты-mcp)
- [HTTP Контракт Streamable](#http-контракт-streamable)
- [Конфигурация (ENV)](#конфигурация-env)
- [Запуск DEV/PROD](#запуск-devprod)
- [MCP Resources](#mcp-resources)
- [Tool Schemas](#tool-schemas)
- [MCP Tools](#mcp-tools)
- [MCP Prompt Templates](#mcp-prompt-templates)
- [Безопасность и Guardrails](#безопасность-и-guardrails)
- [Observability](#observability)
- [Roadmap](#roadmap)
- [FAQ / Troubleshooting](#faq--troubleshooting)
- [Hands-on сценарии](#hands-on-сценарии)

---

## Что это и зачем

### What

Проект превращает OpenAPI-спеку в runtime-интерфейс для AI-агента:

- агент читает ресурсы (`swagger:*`),
- строит запросы tools’ами,
- выполняет вызовы только через `swagger.http.execute`.

### Why

Решает практические проблемы:

- единый канал выполнения реальных API-вызовов,
- контроль auth, политики, лимитов, аудита,
- меньше риска произвольных/опасных вызовов,
- прозрачный контракт: факт ответа сравнивается со Swagger.

### How

Коротко: **resource discovery -> request prepare/validate -> controlled execute -> response validate**.

---

## Архитектура

### Принципы

- `transport -> usecase` через интерфейсы (`usecase.Service`).
- `usecase` не зависит от MCP SDK и транспорта.
- `tool` не ходит в transport напрямую.
- inbound auth (к MCP) и outbound auth (к upstream) разделены.

### Диаграмма слоёв (ASCII)

```text
Clients/Agents
   | (stdio | HTTP streamable)
   v
+-----------------------------+
| Transport Layer             |
| - server/stdio             |
| - server/streamable        |
|   + inbound OAuth middleware|
+--------------+--------------+
               |
               v
+-----------------------------+
| Usecase Layer               |
| - orchestration only        |
+------+---------+------------+
       |         |
       |         +----------------------------+
       v                                      v
+--------------+                     +------------------+
| Tool Registry|                     | Resource Store   |
| (tools)      |                     | (swagger:* etc)  |
+------+-------+                     +--------+---------+
       |                                      |
       v                                      v
+-----------------------------+       +------------------+
| swagger.http.execute        |       | swagger.Store    |
| - policy evaluator          |       | (cache+resolver) |
| - upstream auth provider    |       +------------------+
| - constrained HTTP client   |
| - audit logger + redaction  |
+-----------------------------+
```

### Где что находится

- Inbound OAuth 2.1 Resource Server: `internal/auth` + middleware на `/mcp`.
- Outbound auth к реальному API: `internal/upstreamauth` (используется tool `swagger.http.execute`).
- Политики: `internal/policy`.
- HTTP ограничения/лимиты: `internal/httpclient`.
- Аудит и редактирование секретов: `internal/audit`.
- Swagger кэш и резолвинг: `internal/swagger`.

### Кэши

- Swagger cache: lazy-load + TTL (`SWAGGER_CACHE_TTL`) или reload per request (`SWAGGER_RELOAD=true`).
- Inbound OAuth cache:
  - JWKS cache TTL,
  - introspection cache TTL.
- Upstream OAuth CC cache:
  - access token cache TTL.

---

## Транспорты MCP

## STDIO (default) ✅

Когда использовать:

- локальная интеграция,
- MCP как subprocess.

Запуск:

```bash
TRANSPORT=stdio SWAGGER_PATH=./openapi.yaml go run ./cmd/mcp-server
# или через флаги
go run ./cmd/mcp-server --transport=stdio --swagger-path=./openapi.yaml
```

Пример subprocess (Python):

```python
import os
import subprocess

proc = subprocess.Popen(
    ["go", "run", "./cmd/mcp-server"],
    cwd="/srv/mcp-swagger",
    env={**os.environ, "TRANSPORT": "stdio", "SWAGGER_PATH": "./openapi.yaml"},
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
)
```

## Streamable HTTP Transport ✅

Endpoints:

- `GET /healthz` (без auth)
- `POST /mcp` (MCP JSON-RPC, inbound OAuth)
- `GET /mcp` (standalone SSE stream, inbound OAuth)
- `DELETE /mcp` (завершение MCP session, inbound OAuth)

Пример initialize:

```bash
curl -i -X POST http://127.0.0.1:8080/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"curl-client","version":"0.1.0"},"capabilities":{}}}'
```

Выбор транспорта:

```bash
TRANSPORT=stdio
# или
TRANSPORT=streamable
HTTP_ADDR=:8080
```

Что выбрать:

- `stdio` — простой локальный сценарий.
- `streamable` — сетевой сценарий, OAuth, аудит, multi-client.

## HTTP Контракт Streamable ✅

Ниже контракт **фактической реализации** `internal/server/streamable` на базе `mcp.NewStreamableHTTPHandler` (go-sdk v1.3.1).

Поведение `/mcp` частично делегировано SDK handler; после обновления `go-sdk` проверяйте интеграционные тесты `internal/server/streamable/http_handlers_test.go`.

### Пути и методы

| Method | Path | Auth | Назначение |
|---|---|---|---|
| `GET` | `/healthz` | нет | liveness/readiness |
| `GET` | `/metrics` | опционально (`METRICS_AUTH_REQUIRED`) | Prometheus metrics |
| `POST` | `/mcp` | Bearer обязателен | отправка MCP JSON-RPC сообщений |
| `GET` | `/mcp` | Bearer обязателен | standalone SSE stream для server->client сообщений |
| `DELETE` | `/mcp` | Bearer обязателен | закрытие MCP session |

### Обязательные/условные заголовки

- Для всех `/mcp` запросов: `Authorization: Bearer <token>`
- `X-Correlation-Id` (или значение `CORRELATION_ID_HEADER`) опционален; если отсутствует, сервер сгенерирует UUID и вернёт его в response headers
- `POST /mcp`:
  - `Content-Type: application/json`
  - `Accept` должен включать **и** `application/json`, **и** `text/event-stream` (или `*/*`)
- `GET /mcp`:
  - `Accept: text/event-stream`
  - `Mcp-Session-Id: <session-id>` (обязателен)
- `DELETE /mcp`:
  - `Mcp-Session-Id: <session-id>` (обязателен)
- `Mcp-Protocol-Version`:
  - рекомендуется отправлять на `/mcp` запросах;
  - при отсутствии используется fallback SDK;
  - в текущих интеграционных тестах явно проверена версия `2025-06-18`.

### Коды ответов

- `/healthz`:
  - `200` — OK
  - `405` — метод не `GET`
- `/mcp`:
  - `200` — успешный `POST` call/request-response или активный `GET` stream
  - `202` — `POST` с notifications-only (без call)
  - `204` — успешный `DELETE` (session terminated)
  - `400` — bad request (Accept/headers/body/protocol version/invalid payload)
  - `401` — отсутствует/невалидный bearer token
  - `403` — bearer валиден, но недостаточно прав
  - `404` — session не найдена (`Mcp-Session-Id` неизвестен/закрыт)
  - `405` — неподдерживаемый HTTP метод

### Реальные особенности текущей реализации ⚠️

- Режим SDK handler: **stateful**, `JSONResponse=false` (по умолчанию), поэтому `POST /mcp` ответы идут как `text/event-stream`.
- На `initialize` сервер возвращает `Mcp-Session-Id` в HTTP headers; этот ID нужно передавать в последующих `POST/GET/DELETE /mcp`.
- Event replay через `Last-Event-ID` не включён (EventStore не настроен).
- Интеграционные тесты в репозитории покрывают `GET /healthz` и `POST /mcp` (`initialize`); остальные HTTP-варианты `/mcp` зависят от SDK и должны проверяться при апдейтах.
- CORS:
  - выключен по умолчанию;
  - при включении (`CORS_ALLOWED_ORIGINS`) preflight `OPTIONS` обслуживается только для разрешённых origin.

### Полный цикл по HTTP (initialize -> initialized -> tools/resources -> close)

```bash
TOKEN="<oauth-bearer>"
BASE="http://127.0.0.1:8080"

# 1) initialize
curl -sS -D /tmp/mcp-init.hdr -o /tmp/mcp-init.body \
  -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"curl-client","version":"0.1.0"},"capabilities":{}}}'

SESSION_ID="$(awk 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ {print $2}' /tmp/mcp-init.hdr | tr -d '\r')"
echo "SESSION_ID=$SESSION_ID"

# 2) notifications/initialized (обычно 202 Accepted)
curl -sS -i \
  -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'

# 3) tools/list (SSE ответ с JSON-RPC сообщением в data:)
curl -sS -i \
  -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

# 4) resources/list
curl -sS -i \
  -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}'

# 5) close session
curl -sS -i \
  -X DELETE "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SESSION_ID"
```

Ограничения:

- Legacy SSE transport endpoint (типа `/sse`) не поддерживается.
- `POST /mcp` с `Accept: application/json` без `text/event-stream` отклоняется (`400`).
- `GET /mcp` без `Mcp-Session-Id` отклоняется (`400`).

---

## Конфигурация (ENV)

### CLI аргументы

Сервер можно запускать как CLI-инструмент и передавать конфиг через флаги.

```bash
go run ./cmd/mcp-server \
  --transport=stdio \
  --swagger-path=./openapi.yaml \
  --mcp-api-mode=plan_only
```

Поддерживаемые именованные флаги:

- `--transport`
- `--http-addr`
- `--version`
- `--log-level`
- `--swagger-path`
- `--swagger-format`
- `--swagger-base-url`
- `--swagger-reload`
- `--mcp-api-mode`
- `--upstream-base-url`
- `--upstream-sandbox-base-url`

Для любого параметра из ENV-таблиц используйте repeatable `--set KEY=VALUE`:

```bash
go run ./cmd/mcp-server \
  --swagger-path=https://specs.example.com/openapi.yaml \
  --set SWAGGER_ALLOWED_HOSTS=specs.example.com \
  --set BLOCK_PRIVATE_NETWORKS=true \
  --set INBOUND_OAUTH_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
```

Приоритет источников конфигурации:

1. ENV
2. `--set KEY=VALUE`
3. Именованные CLI-флаги

Важно: inbound и outbound OAuth конфиги теперь независимы:

- **Inbound** (MCP как Resource Server): `INBOUND_OAUTH_*`
- **Outbound** (MCP как OAuth client к upstream): `UPSTREAM_OAUTH_*`

### Обратная совместимость

Поддерживается fallback со старых `OAUTH_*` переменных.

Поведение:

- если новый `INBOUND_OAUTH_*`/`UPSTREAM_OAUTH_*` задан — он имеет приоритет,
- если новый не задан, может использоваться legacy `OAUTH_*`,
- при fallback пишется warning в лог.

## A) Server / Transport

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `TRANSPORT` | `stdio` | `stdio\|streamable` | `streamable` | Выбор транспорта |
| `HTTP_ADDR` | `:8080` | host:port | `0.0.0.0:8080` | Адрес HTTP server |
| `VERSION` | `dev` | string | `1.4.0` | Версия сервиса |
| `LOG_LEVEL` | `info` | `debug\|info\|warn\|error` | `debug` | Уровень логов |
| `CORRELATION_ID_HEADER` | `X-Correlation-Id` | string | `X-Request-Id` | Имя correlation header для входящих/исходящих HTTP вызовов |
| `HTTP_READ_TIMEOUT` | `15s` | duration | `20s` | Read timeout |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | duration | `5s` | Header timeout |
| `HTTP_WRITE_TIMEOUT` | `30s` | duration | `45s` | Write timeout |
| `HTTP_IDLE_TIMEOUT` | `60s` | duration | `120s` | Idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | duration | `15s` | Graceful shutdown |
| `HTTP_SESSION_TIMEOUT` | `2m` | duration | `10m` | MCP session timeout |
| `HTTP_MAX_BODY_BYTES` | `1048576` | int64 | `2097152` | Ограничение body для `/mcp` |
| `METRICS_AUTH_REQUIRED` | `false` | bool | `true` | Требовать Bearer auth для `GET /metrics` |

## B) Swagger / OpenAPI

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `SWAGGER_PATH` | `""` | local path / URL | `./openapi.yaml` или `https://specs.example.com/openapi.yaml` | Источник спек |
| `SWAGGER_FORMAT` | `auto` | `auto\|json\|yaml` | `yaml` | Режим парсинга |
| `SWAGGER_BASE_URL` | `""` | URL | `https://api.example.com` | override base URL в resolver |
| `SWAGGER_RELOAD` | `false` | bool | `true` | reload на каждый запрос |
| `SWAGGER_CACHE_TTL` | `5m` | duration | `10m` | TTL кэша swagger |
| `SWAGGER_HTTP_TIMEOUT` | `10s` | duration | `15s` | timeout HTTP-загрузки swagger по URL |
| `SWAGGER_MAX_BYTES` | `5242880` | int64 bytes | `10485760` | лимит размера swagger payload (file и URL) |
| `SWAGGER_USER_AGENT` | `MCP-Swagger-Loader/1.0` | string | `MCP-Swagger-Loader/2.0` | User-Agent при HTTP-загрузке swagger |
| `SWAGGER_ALLOWED_HOSTS` | `""` | csv | `raw.githubusercontent.com,specs.example.com` | allowlist hosts для `SWAGGER_PATH` URL |

`SWAGGER_FORMAT=auto`:

1. По расширению файла или URL path (`.json`, `.yaml`, `.yml`)
2. Затем попытка JSON -> YAML

`SWAGGER_PATH`:

- локальный путь: `./openapi.yaml`, `/etc/spec/openapi.json`;
- URL: только `http://` или `https://`;
- `file://` и другие схемы отклоняются.
- для URL редиректы разрешены только на policy-валидные target URL и ограничены `5` hops.

## C) Inbound OAuth (MCP Resource Server)

### Общие inbound параметры

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `INBOUND_OAUTH_ISSUER` | `""` | URL/string | `https://issuer.example.com` | Проверка `iss` |
| `INBOUND_OAUTH_AUDIENCE` | `""` | string | `mcp-api` | Проверка `aud` |
| `INBOUND_OAUTH_REQUIRED_SCOPES` | `""` | csv/space list | `mcp:tools.call` | required scopes |

### JWKS режим

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `INBOUND_OAUTH_JWKS_URL` | `""` | URL | `https://issuer/.well-known/jwks.json` | включает JWT/JWKS mode |
| `INBOUND_OAUTH_JWKS_CACHE_TTL` | `5m` | duration | `2m` | TTL JWKS cache |

Проверки:

- подпись JWT (`RS256`, `ES256`)
- `iss`, `aud` (если заданы)
- `exp`, `nbf`
- scopes в `scope`, `scp`, `permissions`

### Introspection режим (RFC 7662)

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `INBOUND_OAUTH_INTROSPECTION_URL` | `""` | URL | `https://issuer/oauth2/introspect` | включает introspection mode |
| `INBOUND_OAUTH_CLIENT_ID` | `""` | string | `mcp-resource-server` | Basic auth к introspection |
| `INBOUND_OAUTH_CLIENT_SECRET` | `""` | secret | `***` | Basic auth к introspection |
| `INBOUND_OAUTH_INTROSPECTION_CACHE_TTL` | `45s` | duration | `30s` | TTL introspection cache |

HTTP-коды:

- `401` — токен отсутствует/невалиден
- `403` — токен валиден, но scope недостаточно

## D) Upstream auth (MCP -> реальный API)

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `UPSTREAM_AUTH_MODE` | `none` | `none\|oauth_client_credentials\|static_bearer\|api_key` | `oauth_client_credentials` | режим auth к upstream |
| `UPSTREAM_BEARER_TOKEN` | `""` | token | `eyJ...` | static bearer token |
| `UPSTREAM_API_KEY_HEADER` | `X-API-Key` | header | `X-API-Key` | API key header name |
| `UPSTREAM_API_KEY_VALUE` | `""` | secret | `***` | API key value |
| `UPSTREAM_OAUTH_TOKEN_URL` | `""` | URL | `https://issuer/oauth/token` | OAuth CC token URL |
| `UPSTREAM_OAUTH_CLIENT_ID` | `""` | string | `upstream-client` | OAuth CC client id |
| `UPSTREAM_OAUTH_CLIENT_SECRET` | `""` | secret | `***` | OAuth CC client secret |
| `UPSTREAM_OAUTH_SCOPES` | `""` | space list | `read write` | OAuth CC scopes |
| `UPSTREAM_OAUTH_AUDIENCE` | `""` | string | `api://upstream` | OAuth CC audience |
| `UPSTREAM_OAUTH_TOKEN_CACHE_TTL` | `0` | duration | `50s` | override token cache TTL |

## E) Policies / Guardrails

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `MCP_API_MODE` | `plan_only` | `plan_only\|execute_readonly\|execute_write\|sandbox` | `execute_readonly` | режим выполнения execute |
| `UPSTREAM_BASE_URL` | `""` | URL | `https://api.example.com` | override base URL |
| `UPSTREAM_SANDBOX_BASE_URL` | `""` | URL | `https://staging-api.example.com` | sandbox base URL |
| `UPSTREAM_ALLOWED_HOSTS` | `""` | csv | `api.example.com,staging-api.example.com` | allowlist hosts для upstream вызовов |
| `BLOCK_PRIVATE_NETWORKS` | `true` | bool | `true` | блок private/loopback/link-local hosts и IP |
| `ALLOWED_METHODS` | `GET,HEAD,OPTIONS` | csv | `GET,POST` | allowlist methods |
| `DENIED_METHODS` | `DELETE` | csv | `DELETE,PATCH` | denylist methods |
| `ALLOWED_OPERATION_IDS` | `""` | csv | `getUser,createOrder` | allowlist operationId |
| `DENIED_OPERATION_IDS` | `""` | csv | `deleteUser` | denylist operationId |
| `REQUIRE_CONFIRMATION_FOR_WRITE` | `false` | bool | `true` | write => confirmation_required |
| `CONFIRMATION_TTL` | `10m` | duration | `15m` | TTL для confirmationId в flow подтверждения |
| `VALIDATE_REQUEST` | `true` | bool | `true` | request validation |
| `VALIDATE_RESPONSE` | `true` | bool | `true` | включает встроенную response validation внутри `swagger.http.execute` |

## F) Limits / Timeouts

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `MAX_CALLS_PER_MINUTE` | `60` | int | `120` | rate limit |
| `MAX_CONCURRENT_CALLS` | `10` | int | `20` | concurrency limit |
| `MAX_CALLS_PER_MINUTE_PER_PRINCIPAL` | `MAX_CALLS_PER_MINUTE` | int | `30` | rate limit на principal (`subject` из inbound OAuth, иначе `anonymous`) |
| `MAX_CONCURRENT_CALLS_PER_PRINCIPAL` | `MAX_CONCURRENT_CALLS` | int | `5` | concurrency limit на principal (`subject` из inbound OAuth, иначе `anonymous`) |
| `HTTP_TIMEOUT` | `30s` | duration | `15s` | upstream HTTP timeout |
| `MAX_REQUEST_BYTES` | `1048576` | int64 | `524288` | request body limit |
| `MAX_RESPONSE_BYTES` | `2097152` | int64 | `1048576` | response body limit |
| `USER_AGENT` | `MCP-Swagger-Agent/1.0` | string | `MCP-Gateway/2.0` | upstream user-agent |

## G) Logging / Audit / Redaction

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `AUDIT_LOG` | `true` | bool | `true` | включить audit |
| `REDACT_HEADERS` | `Authorization,Cookie,X-API-Key` | csv | `Authorization,Cookie` | mask headers |
| `REDACT_JSON_FIELDS` | `password,token,secret,apiKey,access_token,refresh_token` | csv | `password,secret` | mask json fields |

## H) CORS

| ENV | Default | Формат | Пример | Влияние |
|---|---|---|---|---|
| `CORS_ALLOWED_ORIGINS` | `""` | csv / `*` | `https://app.example.com` | CORS allowlist |

---

## Запуск DEV/PROD

## DEV

```bash
make tidy
make build
make test
```

### DEV: stdio

```bash
export TRANSPORT=stdio
export SWAGGER_PATH=./openapi.yaml
export MCP_API_MODE=plan_only

go run ./cmd/mcp-server
```

### DEV: streamable + inbound JWKS

```bash
export TRANSPORT=streamable
export HTTP_ADDR=:8080
export SWAGGER_PATH=./openapi.yaml

export INBOUND_OAUTH_ISSUER=https://issuer.example.com
export INBOUND_OAUTH_AUDIENCE=mcp-api
export INBOUND_OAUTH_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
export INBOUND_OAUTH_REQUIRED_SCOPES=mcp:tools.call

go run ./cmd/mcp-server
```

### DEV: streamable + inbound introspection

```bash
export TRANSPORT=streamable
export HTTP_ADDR=:8080
export SWAGGER_PATH=./openapi.yaml

export INBOUND_OAUTH_INTROSPECTION_URL=https://issuer.example.com/oauth2/introspect
export INBOUND_OAUTH_CLIENT_ID=mcp-resource-server
export INBOUND_OAUTH_CLIENT_SECRET=secret
export INBOUND_OAUTH_REQUIRED_SCOPES=mcp:tools.call

go run ./cmd/mcp-server
```

## Docker

```bash
docker build -t mcp-swagger-gateway .
```

Запуск с env file:

```bash
docker run --rm -p 8080:8080 --env-file .env mcp-swagger-gateway
```

### Пример `.env`

```bash
TRANSPORT=streamable
HTTP_ADDR=:8080
VERSION=1.0.0
LOG_LEVEL=info

SWAGGER_PATH=https://specs.example.com/openapi.yaml
SWAGGER_FORMAT=auto
SWAGGER_RELOAD=false
SWAGGER_CACHE_TTL=5m
SWAGGER_HTTP_TIMEOUT=10s
SWAGGER_MAX_BYTES=5242880
SWAGGER_USER_AGENT=MCP-Swagger-Loader/1.0
SWAGGER_ALLOWED_HOSTS=specs.example.com

# inbound OAuth (client -> MCP)
INBOUND_OAUTH_ISSUER=https://issuer.example.com
INBOUND_OAUTH_AUDIENCE=mcp-api
INBOUND_OAUTH_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
INBOUND_OAUTH_REQUIRED_SCOPES=mcp:tools.call

MCP_API_MODE=execute_readonly
ALLOWED_METHODS=GET,HEAD,OPTIONS
DENIED_METHODS=DELETE
CONFIRMATION_TTL=10m
UPSTREAM_ALLOWED_HOSTS=api.example.com,staging-api.example.com
BLOCK_PRIVATE_NETWORKS=true
MAX_CALLS_PER_MINUTE=60
MAX_CONCURRENT_CALLS=10
HTTP_TIMEOUT=20s

# outbound OAuth (MCP -> upstream API)
UPSTREAM_AUTH_MODE=oauth_client_credentials
UPSTREAM_OAUTH_TOKEN_URL=https://issuer.example.com/oauth/token
UPSTREAM_OAUTH_CLIENT_ID=upstream-client
UPSTREAM_OAUTH_CLIENT_SECRET=upstream-secret
UPSTREAM_OAUTH_SCOPES=read
UPSTREAM_OAUTH_AUDIENCE=api://upstream

AUDIT_LOG=true
REDACT_HEADERS=Authorization,Cookie,X-API-Key
REDACT_JSON_FIELDS=password,token,secret,apiKey
```

## PROD: systemd (пример)

```ini
[Unit]
Description=MCP Swagger Gateway
After=network.target

[Service]
WorkingDirectory=/opt/mcp-swagger
ExecStart=/opt/mcp-swagger/mcp-server
EnvironmentFile=/etc/mcp-swagger.env
Restart=always
RestartSec=3
User=mcp
Group=mcp
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

## PROD: Kubernetes (коротко)

- readiness/liveness: `GET /healthz`
- секреты (`INBOUND_OAUTH_CLIENT_SECRET`, `UPSTREAM_OAUTH_CLIENT_SECRET`, `UPSTREAM_API_KEY_VALUE`) хранить в `Secret`
- для write-профилей: отдельный deployment и отдельные credentials

---

## MCP Resources ✅

## Каталог ✅

- `swagger:endpoints`
- `swagger:endpoints:{METHOD}`
- `swagger:endpointByOperationId:{operationId}`
- `swagger:schema:{name}`
- `swagger:lookup:{pointer}`
- `docs:tool-schemas`

## `swagger:endpoints` ✅

Назначение: вернуть все resolved endpoints.

Пример:

```json
[
  {
    "method": "GET",
    "baseURL": "https://api.example.com",
    "pathTemplate": "/users/{id}",
    "urlTemplate": "https://api.example.com/users/{id}",
    "operationId": "getUserById",
    "summary": "Get user",
    "pathParams": [{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],
    "request": {"contentTypes":["application/json"],"bodySchema":null},
    "responses": {
      "success": [{"status":200,"contentTypes":["application/json"],"bodySchema":{"type":"object"}}],
      "errors": [{"status":404,"contentTypes":["application/json"],"bodySchema":{"type":"object"}}]
    },
    "security": [{"bearerAuth":[]}],
    "servers": ["https://api.example.com"]
  }
]
```

## `swagger:endpoints:{METHOD}` ✅

Пример: `swagger:endpoints:GET`

Ответ: массив `ResolvedOperation` только для указанного метода.

## `swagger:endpointByOperationId:{operationId}` ✅

Пример: `swagger:endpointByOperationId:getUserById`

Ответ: один `ResolvedOperation`.

## `swagger:schema:{name}` ✅

Пример: `swagger:schema:User`

Ответ: schema из `components.schemas.User`.

## `swagger:lookup:{pointer}` ✅

Примеры pointer:

- `/components/schemas/User`
- `/paths/~1users~1{id}/get`
- URL encoded: `%2Fcomponents%2Fschemas%2FUser`

Ответ: любой объект из Swagger.

## `docs:tool-schemas` ✅

Назначение: формальные JSON Schema входа/выхода для MCP tools.

URI: `docs://tool-schemas`

Ответ:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "MCP Swagger Gateway Tool Schemas",
  "tools": {
    "swagger.http.execute": {
      "inputSchema": {"type":"object","required":["operationId"]},
      "outputSchema": {"type":"object","required":["ok","data","error"]}
    }
  }
}
```

### Формат endpoint DTO

Поля (минимум):

- method/baseURL/pathTemplate/urlTemplate/(optional)exampleURL/operationId
- summary/description/tags/deprecated
- path/query/header/cookie params
- request: contentTypes/bodySchema/examples
- responses.success / responses.errors
- security / servers

### Что значит «вложенные схемы раскрыты» ⚠️

- `$ref` раскрывается в JSON объект,
- `allOf/oneOf/anyOf` сохраняются как структура, но вложенные `$ref` внутри раскрываются,
- циклы обозначаются техническими маркерами (например `x-circularRef`).

---

## Tool Schemas ✅

- Канонический источник схем: `internal/tool/schemas.go`.
- Публикация для агентов:
  - через resource `docs:tool-schemas` (`docs://tool-schemas`);
  - через `tools/list` (SDK получает `inputSchema` и `outputSchema`).
- Покрытые tools:
  - `swagger.search`
  - `swagger.plan_call`
  - `swagger.http.generate_payload`
  - `swagger.http.prepare_request`
  - `swagger.http.validate_request`
  - `swagger.http.execute`
  - `swagger.http.validate_response`
  - `policy.request_confirmation`
  - `policy.confirm`

Схема результата для swagger/policy tools унифицирована:

```json
{
  "type": "object",
  "required": ["ok", "data", "error"],
  "properties": {
    "ok": {"type":"boolean"},
    "data": {},
    "error": {
      "anyOf": [
        {"type":"null"},
        {"type":"object","required":["code","message"]}
      ]
    }
  }
}
```

---

## MCP Tools ✅

Единый JSON-контракт для всех `swagger.*` tools ✅:

```json
{
  "operationId": "getUserById",
  "params": {
    "path": {"id": "123"},
    "query": {"verbose": true},
    "headers": {"X-Correlation-Id": "req-001"},
    "body": null
  }
}
```

`operationId` обязателен для `generate_payload`, `prepare_request`, `validate_request`, `execute`, `validate_response`.
Для `search` и `plan_call` можно передавать фильтры в `params.query`.
⚠️ Legacy-формат (`pathParams`, `queryParams`, `body`, `headers` на верхнем уровне) поддерживается как backward-compatible adapter.

Общий формат ответа:

```json
{
  "ok": true,
  "data": {},
  "error": null
}
```

Ошибки:

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "policy_denied",
    "message": "...",
    "details": {}
  }
}
```

Коды ошибок:

- `plan_only`
- `policy_denied`
- `confirmation_required`
- `invalid_request`
- `network_error`
- `timeout`
- `rate_limited`
- `upstream_error`
- `no_base_url`

Для write-flow с `REQUIRE_CONFIRMATION_FOR_WRITE=true` доступны дополнительные tools:

- `policy.request_confirmation`
- `policy.confirm`

## `swagger.search` ✅

Вход:

```json
{
  "params": {
    "query": {
      "query": "user",
      "method": "GET",
      "tag": "users",
      "schema": "User",
      "status": 404,
      "include": ["endpoints", "schemas", "usage"],
      "limit": 20
    }
  }
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "count": 1,
    "filters": {"query":"user","method":"GET","tag":"users","schema":"User","status":404,"include":["endpoints","schemas","usage"],"limit":20},
    "results": [{
      "operationId":"getUserById",
      "method":"GET",
      "baseURL":"https://api.example.com",
      "pathTemplate":"/users/{id}",
      "urlTemplate":"https://api.example.com/users/{id}",
      "matchReason":["query matched operationId","schema used in response body","matches error response status 404"],
      "score": 9.5
    }],
    "schemas": [{"schema":"User","endpoints":[{"operationId":"getUserById","request":false,"response":true}]}],
    "usage": {"schema":{"name":"User"},"status":{"code":404}}
  },
  "error": null
}
```

Новые возможности `swagger.search`:

- поиск endpoints по использованию схем (`schema: "User"`),
- поиск endpoints по status code (`status: 404`) с акцентом на error responses,
- `include` управляет секциями ответа:
  - `endpoints` — ранжированный список операций,
  - `schemas` — агрегаты по найденным схемам,
  - `usage` — индексы использования schema/status.

Если в `params.headers` не передан `X-Correlation-Id` (или header из `CORRELATION_ID_HEADER`), tool добавляет его автоматически.

## `swagger.plan_call` ✅

Вход:

```json
{
  "operationId":"getUserById",
  "params": {
    "query": {"goal":"Получить пользователя"}
  }
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "operation": {"operationId":"getUserById","method":"GET","baseURL":"https://api.example.com","pathTemplate":"/users/{id}","urlTemplate":"https://api.example.com/users/{id}"},
    "steps": [
      {"step":1,"tool":"swagger.http.prepare_request"},
      {"step":2,"tool":"swagger.http.validate_request"},
      {"step":3,"tool":"swagger.http.execute"},
      {"step":4,"tool":"swagger.http.validate_response"}
    ]
  },
  "error": null
}
```

## `swagger.http.generate_payload` ✅

Назначение: сгенерировать `params.body` по request body schema выбранной операции.

Вход:

```json
{
  "operationId":"createUser",
  "params": {
    "query": {
      "strategy":"minimal",
      "seed": 42,
      "overrides": {
        "role":"user"
      }
    }
  }
}
```

Где:

- `strategy`:
  - `minimal` — только required-поля;
  - `example` — предпочитает `example/examples`;
  - `maximal` — старается заполнить больше полей.
- `seed` — детерминирует генерацию значений.
- `overrides` — патч поверх сгенерированного body.

Выход:

```json
{
  "ok": true,
  "data": {
    "operationId":"createUser",
    "strategy":"minimal",
    "seed":42,
    "contentTypes":["application/json"],
    "body":{"name":"value-72305","role":"admin"},
    "warnings":[]
  },
  "error": null
}
```

Поведение генератора:

- заполняет required-поля;
- учитывает `enum`, `minLength`, `minimum`, `format` (email/uuid/date/date-time/uri/ip);
- поддерживает `object`, `array`, `primitive`;
- для `oneOf/anyOf` выбирает вариант детерминированно и возвращает warning;
- для `allOf` объединяет объектные части.

## `swagger.http.prepare_request` ✅

Вход:

```json
{
  "operationId":"getUserById",
  "params":{
    "path":{"id":"123"},
    "query":{"verbose":"true"},
    "headers":{"X-Correlation-Id":"req-001"},
    "body":null
  },
  "contentType":"application/json",
  "baseURL":"https://api.example.com"
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "operationId":"getUserById",
    "method":"GET",
    "finalURL":"https://api.example.com/users/123?verbose=true",
    "validation":{"valid":true,"errors":[]}
  },
  "error": null
}
```

## `swagger.http.validate_request` ✅

Вход: как `prepare_request`.

Выход:

```json
{
  "ok": true,
  "data": {
    "operationId":"getUserById",
    "valid": false,
    "errors": ["missing required path parameter \"id\""]
  },
  "error": null
}
```

## `swagger.http.execute` ✅

Вход:

```json
{
  "operationId":"getUserById",
  "confirmationId":"<optional-approved-confirmation-id>",
  "params":{
    "path":{"id":"123"},
    "query":{"verbose":"true"},
    "headers":{"X-Correlation-Id":"req-001"},
    "body": null
  }
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "operationId":"getUserById",
    "method":"GET",
    "finalURL":"https://api.example.com/users/123?verbose=true",
    "status":200,
    "headers":{"Content-Type":"application/json"},
    "contentType":"application/json",
    "bodyEncoding":"json",
    "body":{"id":"123","email":"user@example.com"},
    "durationMs":42,
    "responseValidation":{"valid":true,"errors":[]}
  },
  "error": null
}
```

Формат тела ответа (строгий):

- `contentType: string`
- `bodyEncoding: "json" | "text" | "base64"`
- `body: any|string`

Правила декодирования:

1. Если `Content-Type` содержит `json` или payload похож на JSON и успешно парсится -> `bodyEncoding=json`
2. Иначе если payload валиден как UTF-8 текст -> `bodyEncoding=text`
3. Иначе -> `bodyEncoding=base64`

Пример `text` ответа:

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "contentType": "text/plain; charset=utf-8",
    "bodyEncoding": "text",
    "body": "pong\n"
  },
  "error": null
}
```

Пример бинарного ответа:

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "contentType": "application/octet-stream",
    "bodyEncoding": "base64",
    "body": "iVBORw0KGgoAAAANSUhEUgAA..."
  },
  "error": null
}
```

### Порядок выполнения в `execute`

1. Получение endpoint по `operationId`
2. Выбор baseURL (`sandbox` -> `UPSTREAM_SANDBOX_BASE_URL`, потом `UPSTREAM_BASE_URL`, потом `endpoint.baseURL`/`servers`)
3. Policy evaluate
4. Request validation (если включено)
5. Apply upstream auth
6. HTTP call через constrained client
7. Response read/decode с лимитами
8. Response validation (если включено)
9. Audit log

### Режимы `MCP_API_MODE`

- `plan_only`: execute запрещён
- `execute_readonly`: обычно только GET/HEAD/OPTIONS
- `execute_write`: write разрешается по policy
- `sandbox`: принудительно sandbox base URL

### `confirmation_required`

Если `REQUIRE_CONFIRMATION_FOR_WRITE=true`, write-метод вернёт:

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "confirmation_required",
    "message": "method \"POST\" requires explicit user confirmation",
    "details": {
      "operationId": "createUser",
      "method": "POST",
      "finalURL": "https://api.example.com/users",
      "recommended_action": "call policy.request_confirmation then policy.confirm then retry swagger.http.execute with confirmationId",
      "next_tool": "policy.request_confirmation"
    }
  }
}
```

Далее используйте flow подтверждения:

1. `policy.request_confirmation`
2. `policy.confirm` (approve=`true`)
3. повторный `swagger.http.execute` с `confirmationId`

## `swagger.http.validate_response` ✅

Вход:

```json
{
  "operationId":"getUserById",
  "params":{
    "query":{"status":200},
    "headers":{"Content-Type":"application/json"},
    "body":{"id":"123"}
  }
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "operationId":"getUserById",
    "status":200,
    "contentType":"application/json",
    "bodyEncoding":"json",
    "body":{"id":"123"},
    "valid": true,
    "errors":[]
  },
  "error": null
}
```

`swagger.http.validate_response` использует тот же формат `contentType/bodyEncoding/body`, что и `swagger.http.execute`.

## `policy.request_confirmation` ✅

Назначение: создать подтверждение для потенциально опасного write-вызова.

Вход:

```json
{
  "operationId": "createUser",
  "reason": "method \"POST\" requires explicit user confirmation",
  "preparedRequestSummary": {
    "operationId": "createUser",
    "method": "POST",
    "finalURL": "https://api.example.com/users"
  }
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "confirmationId": "7b30e9f2a4f748f4b1f95d51f56e8f9f",
    "expiresAt": "2026-02-26T12:30:00Z",
    "summary": {
      "operationId": "createUser",
      "method": "POST",
      "finalURL": "https://api.example.com/users",
      "reason": "method \"POST\" requires explicit user confirmation"
    }
  },
  "error": null
}
```

## `policy.confirm` ✅

Назначение: утвердить или отклонить ранее созданный confirmation request.

Вход:

```json
{
  "confirmationId": "7b30e9f2a4f748f4b1f95d51f56e8f9f",
  "approve": true
}
```

Выход:

```json
{
  "ok": true,
  "data": {
    "confirmationId": "7b30e9f2a4f748f4b1f95d51f56e8f9f",
    "approved": true,
    "expiresAt": "2026-02-26T12:30:00Z"
  },
  "error": null
}
```

Пример (`text`):

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "contentType": "text/plain; charset=utf-8",
    "bodyEncoding": "text",
    "body": "ok",
    "valid": true,
    "errors": []
  },
  "error": null
}
```

Пример (`base64`):

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "contentType": "application/octet-stream",
    "bodyEncoding": "base64",
    "body": "3q2+7w==",
    "valid": true,
    "errors": []
  },
  "error": null
}
```

### Семантика `execute` vs `validate_response`

Принято поведение (Вариант A):

- `swagger.http.execute`:
  - выполняет реальный HTTP вызов;
  - если `VALIDATE_RESPONSE=true`, делает встроенную проверку ответа и кладёт результат в `data.responseValidation`;
  - **не падает** из-за mismatch контракта (то есть `ok` остаётся `true`, а несоответствия идут в `responseValidation.errors`).
- `swagger.http.validate_response`:
  - отдельный инструмент для явной/повторной проверки уже полученного ответа;
  - возвращает нормализованное тело (`contentType/bodyEncoding/body`) + результат валидации (`valid/errors`) и не выполняет HTTP вызов.

Когда использовать:

- нужен реальный вызов API + диагностика расхождений: `swagger.http.execute`;
- нужно перепроверить/сравнить ответ отдельно (например после пост-обработки тела): `swagger.http.validate_response`.

---

## MCP Prompt Templates ✅

## `swagger.call_agent` ✅

Назначение: задаёт безопасный workflow агенту.

Текущий workflow в шаблоне (5 шагов):

1. Уточнить цель пользователя и риск.
2. Найти операции (`swagger.search`).
3. Спланировать вызов (`swagger.plan_call`).
4. Подготовить запрос (`swagger.http.prepare_request`).
5. Проверить запрос (`swagger.http.validate_request`), выполнить вызов (`swagger.http.execute`) и проверить ответ (`swagger.http.validate_response`).

⚠️ Шаблон `swagger.call_agent` по умолчанию не вставляет отдельный шаг `policy.request_confirmation`/`policy.confirm`; для write-сценариев агент должен добавить этот flow самостоятельно.

Псевдо-диалог:

```text
Agent -> swagger.search(...)
Agent -> swagger.plan_call(...)
Agent -> swagger.http.prepare_request(...)
Agent -> swagger.http.validate_request(...)
Agent -> policy.request_confirmation(...) [опционально для write]
Agent -> policy.confirm(...) [опционально для write]
Agent -> swagger.http.execute(...)
Agent -> swagger.http.validate_response(...)
```

---

## Безопасность и Guardrails ✅

### Почему execute опасен без ограничений

Риски:

- data exfiltration,
- массовые write-операции,
- утечки секретов в логах,
- destructive действия без контроля.

### Рекомендации для production

1. Default держать `plan_only`.
2. Для read use-case: `execute_readonly`.
3. Для write:
   - `ALLOWED_OPERATION_IDS` обязателен,
   - `DENIED_METHODS=DELETE` по умолчанию,
   - `REQUIRE_CONFIRMATION_FOR_WRITE=true`.
4. Разделяйте inbound и outbound credentials.
5. Включайте audit + redaction.
6. Включайте rate/concurrency/size лимиты.
7. Для тестов включайте `sandbox`.

### Приоритеты Policy (детерминированный порядок) ✅

Порядок принятия решения в `internal/policy`:

| Шаг | Проверка | Результат при срабатывании |
|---|---|---|
| 1 | `DENIED_OPERATION_IDS` | deny: `policy_denied` |
| 2 | `DENIED_METHODS` | deny: `policy_denied` |
| 3 | `ALLOWED_OPERATION_IDS` (если задан) | если `operationId` не в allowlist -> deny |
| 4 | `ALLOWED_METHODS` (если задан) | если method не в allowlist -> deny |
| 5 | `MCP_API_MODE` | `plan_only` -> deny `plan_only`; `execute_readonly` -> deny write-methods; `execute_write/sandbox` -> pass |
| 6 | `REQUIRE_CONFIRMATION_FOR_WRITE` | для write-methods -> deny `confirmation_required` |

Примечания:

- deny-правила всегда перекрывают allow-правила.
- allowlist по `operationId` ограничивает вызовы только перечисленными операциями.
- `execute_readonly` дополнительно ограничивает методы до `GET/HEAD/OPTIONS`, даже если `ALLOWED_METHODS` шире.

Примеры:

1. `DENIED_OPERATION_IDS=createUser`, `ALLOWED_OPERATION_IDS=createUser`, method=`POST`  
   результат: deny на шаге 1 (`operationId explicitly denied`).
2. `DENIED_METHODS=POST`, `ALLOWED_METHODS=POST`, mode=`execute_write`  
   результат: deny на шаге 2 (`HTTP method explicitly denied`).
3. mode=`execute_readonly`, `ALLOWED_METHODS=POST`, method=`POST`  
   результат: deny на шаге 5 (readonly mode запрет write).
4. mode=`execute_write`, `REQUIRE_CONFIRMATION_FOR_WRITE=true`, method=`POST`  
   результат: deny на шаге 6 (`confirmation_required`).

### Мини-модель угроз

- Prompt injection через Swagger descriptions
- SSRF через base URL override
- Credential leakage
- Mass-write

Контрмеры: policy, auth split, allow/deny, sandbox, audit/redaction, лимиты.

### SSRF защита и allowlist хостов ✅

Реализованы два уровня защиты:

1. **Fail-fast на старте**:
   - валидируются `UPSTREAM_BASE_URL`, `UPSTREAM_SANDBOX_BASE_URL`, `SWAGGER_BASE_URL`,
   - если `SWAGGER_PATH` это URL — валидируется host,
   - после загрузки Swagger валидируются `servers`/`baseURL` всех операций.
2. **Defense-in-depth перед каждым `swagger.http.execute`**:
   - повторно валидируется выбранный `baseURL`,
   - валидируется итоговый `finalURL`,
   - редиректы разрешены только если target URL проходит ту же проверку host policy.
3. **Безопасная загрузка Swagger по URL**:
   - поддерживаются только `http/https`,
   - редиректы ограничены (`max 5`) и каждый hop валидируется через policy,
   - применяются `SWAGGER_HTTP_TIMEOUT` и `SWAGGER_MAX_BYTES`.

Пример production-настройки:

```bash
SWAGGER_PATH=https://specs.example.com/openapi.yaml
SWAGGER_ALLOWED_HOSTS=specs.example.com

UPSTREAM_BASE_URL=https://api.example.com
UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
UPSTREAM_ALLOWED_HOSTS=api.example.com,staging-api.example.com
BLOCK_PRIVATE_NETWORKS=true
```

Рекомендуется:

- задавать явные allowlist’ы для Swagger и upstream отдельно,
- не использовать wildcard `*` для host allowlist,
- держать `BLOCK_PRIVATE_NETWORKS=true` в production.

---

## Observability

Что логируется:

- ✅ сервисные логи (`slog`)
- ✅ audit записи execute

Пример audit-события:

```json
{
  "timestamp": "2026-02-26T12:00:00Z",
  "principal": "user-123",
  "correlationId": "f4e6a1e7-0d6f-45d2-9bf0-3d7a8f2a0d13",
  "operationId": "getUserById",
  "method": "GET",
  "url": "https://api.example.com/users/123",
  "requestHeaders": {"Authorization": "[REDACTED]"},
  "responseStatus": 200,
  "durationMs": 42,
  "error": ""
}
```

Correlation ID:

- ✅ streamable middleware генерирует correlation id (UUID), если входящий header отсутствует
- ✅ correlation id кладётся в context и возвращается в HTTP response header
- ✅ `swagger.http.execute` прокидывает correlation id в upstream request header (если `params.headers` не содержит его)
- ✅ audit-событие включает поле `correlationId`
- ✅ в stdio-режиме `swagger.http.execute` генерирует correlation id на каждый вызов, если он не передан явно

Метрики:

- ✅ endpoint: `GET /metrics`
- ✅ метрики:
  - `mcp_execute_total{operationId,method,status}`
  - `mcp_execute_errors_total{code}`
  - `mcp_execute_duration_seconds_bucket` (histogram buckets)
  - `mcp_execute_inflight`
  - `mcp_rate_limited_total`

Пример запроса:

```bash
curl -s http://127.0.0.1:8080/metrics | head -n 40
```

Если `METRICS_AUTH_REQUIRED=true`:

```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8080/metrics | head -n 40
```

Пример scrape-конфига Prometheus:

```yaml
scrape_configs:
  - job_name: mcp-swagger-gateway
    metrics_path: /metrics
    static_configs:
      - targets: ["mcp-swagger-gateway:8080"]
```

⚠️ При `METRICS_AUTH_REQUIRED=false` публикуйте `/metrics` только во внутренней/private сети.

---

## Roadmap

- 🧭 Расширение интеграционных тестов streamable (`GET /mcp`, `DELETE /mcp`, CORS preflight сценарии).
- 🧭 Более глубокая нормализация OpenAPI-композиций (`allOf/oneOf/anyOf`) с опциональным merge режимом.

---

## FAQ / Troubleshooting

## Swagger не парсится

```bash
ls -la ./openapi.yaml
curl -I https://example.com/openapi.yaml
export SWAGGER_FORMAT=yaml
```

Проверить source type:

- локальный файл: `SWAGGER_PATH=./openapi.yaml`;
- URL: только `http://`/`https://` (`file://` отклоняется).

Если `SWAGGER_PATH` URL:

- проверить `SWAGGER_ALLOWED_HOSTS`,
- проверить `BLOCK_PRIVATE_NETWORKS`,
- проверить `SWAGGER_HTTP_TIMEOUT`,
- проверить `SWAGGER_MAX_BYTES` (если spec большая).

## `no_base_url`

```bash
export UPSTREAM_BASE_URL=https://api.example.com
# или для sandbox
export MCP_API_MODE=sandbox
export UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
```

## 401/403 на `/mcp`

Проверить inbound OAuth:

- `INBOUND_OAUTH_*` параметры
- issuer/audience/scopes
- валидность bearer токена

## 401/403 на upstream API

Проверить outbound auth:

- `UPSTREAM_AUTH_MODE`
- `UPSTREAM_OAUTH_*` или `UPSTREAM_API_KEY_*` / `UPSTREAM_BEARER_TOKEN`

## Response validation errors

`execute` может быть успешным, но `responseValidation.valid=false`.

Это сигнал рассинхрона Swagger и реального API.

## Timeout / response too large

- увеличить `HTTP_TIMEOUT`
- увеличить `MAX_RESPONSE_BYTES`
- проверить размер ответа endpoint

## `policy_denied`: host blocked by security policy

Причина: URL/host не проходит SSRF guardrails.

Проверьте:

- `UPSTREAM_ALLOWED_HOSTS` (для `execute`) и/или `SWAGGER_ALLOWED_HOSTS` (для `SWAGGER_PATH` URL),
- `BLOCK_PRIVATE_NETWORKS=true` блокирует `127.0.0.1`, RFC1918 и link-local сети,
- редирект целевого API ведёт на разрешённый host.

## Ошибки загрузки `SWAGGER_PATH` по URL

Типовые причины:

- `unsupported swagger URL scheme`: задана схема не `http/https` (например `file://`);
- `swagger url blocked by policy`: host не проходит `SWAGGER_ALLOWED_HOSTS` / private-network policy;
- `swagger redirect blocked by policy`: redirect target не проходит ту же host policy;
- `too many redirects while fetching swagger`: превышен лимит redirect hops (5);
- `swagger payload exceeds configured size limit`: превышен `SWAGGER_MAX_BYTES`.

## `confirmation_required`

Это guardrail для write.

Безопасный путь:

1. Вызвать `policy.request_confirmation`.
2. Получить подтверждение человека и вызвать `policy.confirm` с `approve=true`.
3. Повторить `swagger.http.execute`, передав `confirmationId`.

---

## Hands-on сценарии

## 1) Найти endpoint -> подготовить -> валидировать -> выполнить GET

```json
{"tool":"swagger.search","arguments":{"params":{"query":{"query":"user by id","method":"GET"}}}}
```

```json
{"tool":"swagger.http.prepare_request","arguments":{"operationId":"getUserById","params":{"path":{"id":"123"}}}}
```

```json
{"tool":"swagger.http.validate_request","arguments":{"operationId":"getUserById","params":{"path":{"id":"123"}}}}
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"getUserById","params":{"path":{"id":"123"}}}}
```

## 2) Write endpoint -> confirmation_required -> confirm -> execute

```bash
export MCP_API_MODE=execute_write
export REQUIRE_CONFIRMATION_FOR_WRITE=true
export CONFIRMATION_TTL=10m
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"createUser","params":{"body":{"email":"new@example.com"}}}}
```

Ожидаемо:

```json
{"ok":false,"data":null,"error":{"code":"confirmation_required"}}
```

Создаём confirmation request:

```json
{"tool":"policy.request_confirmation","arguments":{"operationId":"createUser","reason":"method \"POST\" requires explicit user confirmation","preparedRequestSummary":{"operationId":"createUser","method":"POST","finalURL":"https://api.example.com/users"}}}
```

Подтверждаем:

```json
{"tool":"policy.confirm","arguments":{"confirmationId":"<id-from-previous-step>","approve":true}}
```

Повторяем execute с `confirmationId`:

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"createUser","confirmationId":"<id-from-previous-step>","params":{"body":{"email":"new@example.com"}}}}
```

## 3) Sandbox режим

```bash
export MCP_API_MODE=sandbox
export UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"getUserById","params":{"path":{"id":"123"}}}}
```

URL в ответе должен быть sandbox.

## 4) Найти schema и собрать payload

```json
{"resource":"swagger:schema:UserCreateRequest"}
```

```json
{"email":"new@example.com","password":"S3cur3Pass!","name":"Alice"}
```

```json
{"tool":"swagger.http.validate_request","arguments":{"operationId":"createUser","params":{"body":{"email":"new@example.com","password":"S3cur3Pass!","name":"Alice"}}}}
```

## 5) Search -> generate_payload -> validate_request -> execute

```json
{"tool":"swagger.search","arguments":{"params":{"query":{"query":"create user","method":"POST","limit":1}}}}
```

```json
{"tool":"swagger.http.generate_payload","arguments":{"operationId":"createUser","params":{"query":{"strategy":"minimal","seed":42}}}}
```

Пример сокращённого ответа:

```json
{"ok":true,"data":{"body":{"name":"value-72305","role":"admin"},"warnings":[]}}
```

```json
{"tool":"swagger.http.validate_request","arguments":{"operationId":"createUser","params":{"body":{"name":"value-72305","role":"admin"}}}}
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"createUser","params":{"body":{"name":"value-72305","role":"admin"}}}}
```

---

## Команды

```bash
make tidy
make fmt
make lint
make test
make build
make run
```
