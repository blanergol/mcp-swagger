# MCP Swagger Gateway (Go)

An MCP server in Go that:

- loads Swagger/OpenAPI (JSON/YAML, file or URL),
- publishes API structure as MCP resources,
- provides MCP tools for preparing/validating/safely executing real HTTP calls,
- works as a controlled gateway between an AI agent and an upstream API.

## Quick Start

1. Preparation:

```bash
make tidy
make build
```

2. Run in `stdio` (default):

```bash
export TRANSPORT=stdio
export SWAGGER_PATH=./openapi.yaml
export MCP_API_MODE=plan_only
make run
```

Same run via CLI arguments:

```bash
go run ./cmd/mcp-server \
  --transport=stdio \
  --swagger-path=./openapi.yaml \
  --mcp-api-mode=plan_only
```

3. Run with `SWAGGER_PATH` as URL (production-safe):

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

4. Run `streamable` HTTP + inbound OAuth (JWKS example):

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

5. Verification:

```bash
curl -s http://127.0.0.1:8080/healthz
```

---

## Implementation Status (code audit)

| Feature | Status | Comment |
|---|---|---|
| MCP transport `stdio` | ✅ implemented | `internal/server/stdio` |
| MCP transport `streamable HTTP` | ✅ implemented | `/healthz` + `/mcp` via SDK handler |
| HTTP behavior details for `/mcp` (Accept/session/version) | ⚠️ limitation/condition | Behavior is partially determined by `go-sdk` (`mcp.NewStreamableHTTPHandler`) |
| Inbound OAuth 2.1 (JWKS + Introspection) | ✅ implemented | `internal/auth` |
| Outbound auth to upstream (none/static/api_key/oauth_cc) | ✅ implemented | `internal/upstreamauth` |
| Guardrails/policy (mode + allow/deny + confirmation_required) | ✅ implemented | `internal/policy` |
| Human-in-the-loop confirmation flow (`policy.request_confirmation`, `policy.confirm`) | ✅ implemented | In-memory TTL store + check in `swagger.http.execute` |
| SSRF protection (allowlist + private networks + redirect policy) | ✅ implemented | `internal/netguard`, checks at startup and before execute |
| Swagger resources (`swagger:endpoints/...`) | ✅ implemented | `internal/resouce/swagger_store.go` |
| Formal JSON Schema for tools + docs resource | ✅ implemented | `internal/tool/schemas.go` + resource `docs:tool-schemas` |
| Swagger schema resolution (`$ref`) | ✅ implemented | `$ref` are resolved recursively |
| Compositions `allOf/oneOf/anyOf` | ⚠️ limitation/condition | Structures are preserved as-is, nested `$ref` are resolved, no full merge |
| Circular schemas | ⚠️ limitation/condition | `x-circularRef`/`x-unresolvedRef` markers instead of infinite resolution |
| Structured audit logging + redaction | ✅ implemented | `internal/audit` |
| Auto-generation of correlation id + propagation to upstream/audit | ✅ implemented | middleware streamable + fallback in `swagger.http.execute` |
| Built-in Prometheus metrics (`/metrics`) | ✅ implemented | `internal/metrics` + endpoint `GET /metrics` |

## Table of Contents

- [Implementation Status (code audit)](#implementation-status-code-audit)
- [What Is This and Why](#what-is-this-and-why)
- [Architecture](#architecture)
- [MCP Transports](#mcp-transports)
- [HTTP Contract Streamable](#http-contract-streamable)
- [Configuration (ENV)](#configuration-env)
- [Running DEV/PROD](#running-devprod)
- [MCP Resources](#mcp-resources)
- [Tool Schemas](#tool-schemas)
- [MCP Tools](#mcp-tools)
- [MCP Prompt Templates](#mcp-prompt-templates)
- [Security and Guardrails](#security-and-guardrails)
- [Observability](#observability)
- [Roadmap](#roadmap)
- [FAQ / Troubleshooting](#faq--troubleshooting)
- [Hands-on Scenarios](#hands-on-scenarios)

---

## What Is This and Why

### What

The project turns an OpenAPI spec into a runtime interface for an AI agent:

- the agent reads resources (`swagger:*`),
- builds requests using tools,
- executes calls only through `swagger.http.execute`.

### Why

Solves practical problems:

- single channel for executing real API calls,
- control over auth, policies, limits, audit,
- less risk of arbitrary/dangerous calls,
- transparent contract: the response fact is compared against Swagger.

### How

In short: **resource discovery -> request prepare/validate -> controlled execute -> response validate**.

---

## Architecture

### Principles

- `transport -> usecase` via interfaces (`usecase.Service`).
- `usecase` does not depend on MCP SDK or transport.
- `tool` does not access transport directly.
- inbound auth (to MCP) and outbound auth (to upstream) are separated.

### Layer Diagram (ASCII)

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

### Where Things Are Located

- Inbound OAuth 2.1 Resource Server: `internal/auth` + middleware on `/mcp`.
- Outbound auth to the real API: `internal/upstreamauth` (used by tool `swagger.http.execute`).
- Policies: `internal/policy`.
- HTTP constraints/limits: `internal/httpclient`.
- Audit and secret redaction: `internal/audit`.
- Swagger cache and resolution: `internal/swagger`.

### Caches

- Swagger cache: lazy-load + TTL (`SWAGGER_CACHE_TTL`) or reload per request (`SWAGGER_RELOAD=true`).
- Inbound OAuth cache:
  - JWKS cache TTL,
  - introspection cache TTL.
- Upstream OAuth CC cache:
  - access token cache TTL.

---

## MCP Transports

## STDIO (default) ✅

When to use:

- local integration,
- MCP as subprocess.

Running:

```bash
TRANSPORT=stdio SWAGGER_PATH=./openapi.yaml go run ./cmd/mcp-server
# or via flags
go run ./cmd/mcp-server --transport=stdio --swagger-path=./openapi.yaml
```

Subprocess example (Python):

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

- `GET /healthz` (no auth)
- `POST /mcp` (MCP JSON-RPC, inbound OAuth)
- `GET /mcp` (standalone SSE stream, inbound OAuth)
- `DELETE /mcp` (MCP session termination, inbound OAuth)

Initialize example:

```bash
curl -i -X POST http://127.0.0.1:8080/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"curl-client","version":"0.1.0"},"capabilities":{}}}'
```

Transport selection:

```bash
TRANSPORT=stdio
# or
TRANSPORT=streamable
HTTP_ADDR=:8080
```

What to choose:

- `stdio` — simple local scenario.
- `streamable` — network scenario, OAuth, audit, multi-client.

## HTTP Contract Streamable ✅

Below is the contract of the **actual implementation** of `internal/server/streamable` based on `mcp.NewStreamableHTTPHandler` (go-sdk v1.3.1).

The `/mcp` behavior is partially delegated to the SDK handler; after updating `go-sdk`, verify integration tests at `internal/server/streamable/http_handlers_test.go`.

### Paths and Methods

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | none | liveness/readiness |
| `GET` | `/metrics` | optional (`METRICS_AUTH_REQUIRED`) | Prometheus metrics |
| `POST` | `/mcp` | Bearer required | sending MCP JSON-RPC messages |
| `GET` | `/mcp` | Bearer required | standalone SSE stream for server->client messages |
| `DELETE` | `/mcp` | Bearer required | closing MCP session |

### Required/Conditional Headers

- For all `/mcp` requests: `Authorization: Bearer <token>`
- `X-Correlation-Id` (or value of `CORRELATION_ID_HEADER`) is optional; if absent, the server will generate a UUID and return it in response headers
- `POST /mcp`:
  - `Content-Type: application/json`
  - `Accept` must include **both** `application/json` **and** `text/event-stream` (or `*/*`)
- `GET /mcp`:
  - `Accept: text/event-stream`
  - `Mcp-Session-Id: <session-id>` (required)
- `DELETE /mcp`:
  - `Mcp-Session-Id: <session-id>` (required)
- `Mcp-Protocol-Version`:
  - recommended to send on `/mcp` requests;
  - if absent, SDK fallback is used;
  - in current integration tests, version `2025-06-18` is explicitly verified.

### Response Codes

- `/healthz`:
  - `200` — OK
  - `405` — method is not `GET`
- `/mcp`:
  - `200` — successful `POST` call/request-response or active `GET` stream
  - `202` — `POST` with notifications-only (no call)
  - `204` — successful `DELETE` (session terminated)
  - `400` — bad request (Accept/headers/body/protocol version/invalid payload)
  - `401` — missing/invalid bearer token
  - `403` — bearer is valid but insufficient permissions
  - `404` — session not found (`Mcp-Session-Id` unknown/closed)
  - `405` — unsupported HTTP method

### Real Implementation Details ⚠️

- SDK handler mode: **stateful**, `JSONResponse=false` (by default), so `POST /mcp` responses are sent as `text/event-stream`.
- On `initialize`, the server returns `Mcp-Session-Id` in HTTP headers; this ID must be passed in subsequent `POST/GET/DELETE /mcp`.
- Event replay via `Last-Event-ID` is not enabled (EventStore is not configured).
- Integration tests in the repository cover `GET /healthz` and `POST /mcp` (`initialize`); other HTTP variants of `/mcp` depend on the SDK and should be verified during updates.
- CORS:
  - disabled by default;
  - when enabled (`CORS_ALLOWED_ORIGINS`), preflight `OPTIONS` is served only for allowed origins.

### Full HTTP Cycle (initialize -> initialized -> tools/resources -> close)

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

# 2) notifications/initialized (typically 202 Accepted)
curl -sS -i \
  -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'

# 3) tools/list (SSE response with JSON-RPC message in data:)
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

Limitations:

- Legacy SSE transport endpoint (like `/sse`) is not supported.
- `POST /mcp` with `Accept: application/json` without `text/event-stream` is rejected (`400`).
- `GET /mcp` without `Mcp-Session-Id` is rejected (`400`).

---

## Configuration (ENV)

### CLI Arguments

The server can be run as a CLI tool with configuration passed via flags.

```bash
go run ./cmd/mcp-server \
  --transport=stdio \
  --swagger-path=./openapi.yaml \
  --mcp-api-mode=plan_only
```

Supported named flags:

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

For any parameter from the ENV tables, use the repeatable `--set KEY=VALUE`:

```bash
go run ./cmd/mcp-server \
  --swagger-path=https://specs.example.com/openapi.yaml \
  --set SWAGGER_ALLOWED_HOSTS=specs.example.com \
  --set BLOCK_PRIVATE_NETWORKS=true \
  --set INBOUND_OAUTH_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
```

Configuration source priority:

1. ENV
2. `--set KEY=VALUE`
3. Named CLI flags

Important: inbound and outbound OAuth configs are now independent:

- **Inbound** (MCP as Resource Server): `INBOUND_OAUTH_*`
- **Outbound** (MCP as OAuth client to upstream): `UPSTREAM_OAUTH_*`

### Backward Compatibility

Fallback from legacy `OAUTH_*` variables is supported.

Behavior:

- if the new `INBOUND_OAUTH_*`/`UPSTREAM_OAUTH_*` is set — it takes priority,
- if the new one is not set, the legacy `OAUTH_*` may be used,
- a warning is logged on fallback.

## A) Server / Transport

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `TRANSPORT` | `stdio` | `stdio\|streamable` | `streamable` | Transport selection |
| `HTTP_ADDR` | `:8080` | host:port | `0.0.0.0:8080` | HTTP server address |
| `VERSION` | `dev` | string | `1.4.0` | Service version |
| `LOG_LEVEL` | `info` | `debug\|info\|warn\|error` | `debug` | Log level |
| `CORRELATION_ID_HEADER` | `X-Correlation-Id` | string | `X-Request-Id` | Correlation header name for incoming/outgoing HTTP calls |
| `HTTP_READ_TIMEOUT` | `15s` | duration | `20s` | Read timeout |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | duration | `5s` | Header timeout |
| `HTTP_WRITE_TIMEOUT` | `30s` | duration | `45s` | Write timeout |
| `HTTP_IDLE_TIMEOUT` | `60s` | duration | `120s` | Idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | duration | `15s` | Graceful shutdown |
| `HTTP_SESSION_TIMEOUT` | `2m` | duration | `10m` | MCP session timeout |
| `HTTP_MAX_BODY_BYTES` | `1048576` | int64 | `2097152` | Body limit for `/mcp` |
| `METRICS_AUTH_REQUIRED` | `false` | bool | `true` | Require Bearer auth for `GET /metrics` |

## B) Swagger / OpenAPI

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `SWAGGER_PATH` | `""` | local path / URL | `./openapi.yaml` or `https://specs.example.com/openapi.yaml` | Spec source |
| `SWAGGER_FORMAT` | `auto` | `auto\|json\|yaml` | `yaml` | Parsing mode |
| `SWAGGER_BASE_URL` | `""` | URL | `https://api.example.com` | Override base URL in resolver |
| `SWAGGER_RELOAD` | `false` | bool | `true` | Reload on every request |
| `SWAGGER_CACHE_TTL` | `5m` | duration | `10m` | Swagger cache TTL |
| `SWAGGER_HTTP_TIMEOUT` | `10s` | duration | `15s` | HTTP timeout for loading swagger via URL |
| `SWAGGER_MAX_BYTES` | `5242880` | int64 bytes | `10485760` | Swagger payload size limit (file and URL) |
| `SWAGGER_USER_AGENT` | `MCP-Swagger-Loader/1.0` | string | `MCP-Swagger-Loader/2.0` | User-Agent for HTTP swagger loading |
| `SWAGGER_ALLOWED_HOSTS` | `""` | csv | `raw.githubusercontent.com,specs.example.com` | Host allowlist for `SWAGGER_PATH` URL |

`SWAGGER_FORMAT=auto`:

1. By file extension or URL path (`.json`, `.yaml`, `.yml`)
2. Then attempt JSON -> YAML

`SWAGGER_PATH`:

- local path: `./openapi.yaml`, `/etc/spec/openapi.json`;
- URL: only `http://` or `https://`;
- `file://` and other schemes are rejected.
- for URLs, redirects are allowed only to policy-valid target URLs and limited to `5` hops.

## C) Inbound OAuth (MCP Resource Server)

### Common Inbound Parameters

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `INBOUND_OAUTH_ISSUER` | `""` | URL/string | `https://issuer.example.com` | `iss` validation |
| `INBOUND_OAUTH_AUDIENCE` | `""` | string | `mcp-api` | `aud` validation |
| `INBOUND_OAUTH_REQUIRED_SCOPES` | `""` | csv/space list | `mcp:tools.call` | Required scopes |

### JWKS Mode

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `INBOUND_OAUTH_JWKS_URL` | `""` | URL | `https://issuer/.well-known/jwks.json` | Enables JWT/JWKS mode |
| `INBOUND_OAUTH_JWKS_CACHE_TTL` | `5m` | duration | `2m` | JWKS cache TTL |

Validations:

- JWT signature (`RS256`, `ES256`)
- `iss`, `aud` (if set)
- `exp`, `nbf`
- scopes in `scope`, `scp`, `permissions`

### Introspection Mode (RFC 7662)

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `INBOUND_OAUTH_INTROSPECTION_URL` | `""` | URL | `https://issuer/oauth2/introspect` | Enables introspection mode |
| `INBOUND_OAUTH_CLIENT_ID` | `""` | string | `mcp-resource-server` | Basic auth to introspection |
| `INBOUND_OAUTH_CLIENT_SECRET` | `""` | secret | `***` | Basic auth to introspection |
| `INBOUND_OAUTH_INTROSPECTION_CACHE_TTL` | `45s` | duration | `30s` | Introspection cache TTL |

HTTP codes:

- `401` — token is missing/invalid
- `403` — token is valid but scope is insufficient

## D) Upstream Auth (MCP -> Real API)

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `UPSTREAM_AUTH_MODE` | `none` | `none\|oauth_client_credentials\|static_bearer\|api_key` | `oauth_client_credentials` | Auth mode to upstream |
| `UPSTREAM_BEARER_TOKEN` | `""` | token | `eyJ...` | Static bearer token |
| `UPSTREAM_API_KEY_HEADER` | `X-API-Key` | header | `X-API-Key` | API key header name |
| `UPSTREAM_API_KEY_VALUE` | `""` | secret | `***` | API key value |
| `UPSTREAM_OAUTH_TOKEN_URL` | `""` | URL | `https://issuer/oauth/token` | OAuth CC token URL |
| `UPSTREAM_OAUTH_CLIENT_ID` | `""` | string | `upstream-client` | OAuth CC client id |
| `UPSTREAM_OAUTH_CLIENT_SECRET` | `""` | secret | `***` | OAuth CC client secret |
| `UPSTREAM_OAUTH_SCOPES` | `""` | space list | `read write` | OAuth CC scopes |
| `UPSTREAM_OAUTH_AUDIENCE` | `""` | string | `api://upstream` | OAuth CC audience |
| `UPSTREAM_OAUTH_TOKEN_CACHE_TTL` | `0` | duration | `50s` | Override token cache TTL |

## E) Policies / Guardrails

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `MCP_API_MODE` | `plan_only` | `plan_only\|execute_readonly\|execute_write\|sandbox` | `execute_readonly` | Execute execution mode |
| `UPSTREAM_BASE_URL` | `""` | URL | `https://api.example.com` | Override base URL |
| `UPSTREAM_SANDBOX_BASE_URL` | `""` | URL | `https://staging-api.example.com` | Sandbox base URL |
| `UPSTREAM_ALLOWED_HOSTS` | `""` | csv | `api.example.com,staging-api.example.com` | Host allowlist for upstream calls |
| `BLOCK_PRIVATE_NETWORKS` | `true` | bool | `true` | Block private/loopback/link-local hosts and IPs |
| `ALLOWED_METHODS` | `GET,HEAD,OPTIONS` | csv | `GET,POST` | Methods allowlist |
| `DENIED_METHODS` | `DELETE` | csv | `DELETE,PATCH` | Methods denylist |
| `ALLOWED_OPERATION_IDS` | `""` | csv | `getUser,createOrder` | operationId allowlist |
| `DENIED_OPERATION_IDS` | `""` | csv | `deleteUser` | operationId denylist |
| `REQUIRE_CONFIRMATION_FOR_WRITE` | `false` | bool | `true` | write => confirmation_required |
| `CONFIRMATION_TTL` | `10m` | duration | `15m` | TTL for confirmationId in the confirmation flow |
| `VALIDATE_REQUEST` | `true` | bool | `true` | Request validation |
| `VALIDATE_RESPONSE` | `true` | bool | `true` | Enables built-in response validation inside `swagger.http.execute` |

## F) Limits / Timeouts

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `MAX_CALLS_PER_MINUTE` | `60` | int | `120` | Rate limit |
| `MAX_CONCURRENT_CALLS` | `10` | int | `20` | Concurrency limit |
| `MAX_CALLS_PER_MINUTE_PER_PRINCIPAL` | `MAX_CALLS_PER_MINUTE` | int | `30` | Rate limit per principal (`subject` from inbound OAuth, otherwise `anonymous`) |
| `MAX_CONCURRENT_CALLS_PER_PRINCIPAL` | `MAX_CONCURRENT_CALLS` | int | `5` | Concurrency limit per principal (`subject` from inbound OAuth, otherwise `anonymous`) |
| `HTTP_TIMEOUT` | `30s` | duration | `15s` | Upstream HTTP timeout |
| `MAX_REQUEST_BYTES` | `1048576` | int64 | `524288` | Request body limit |
| `MAX_RESPONSE_BYTES` | `2097152` | int64 | `1048576` | Response body limit |
| `USER_AGENT` | `MCP-Swagger-Agent/1.0` | string | `MCP-Gateway/2.0` | Upstream user-agent |

## G) Logging / Audit / Redaction

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `AUDIT_LOG` | `true` | bool | `true` | Enable audit |
| `REDACT_HEADERS` | `Authorization,Cookie,X-API-Key` | csv | `Authorization,Cookie` | Mask headers |
| `REDACT_JSON_FIELDS` | `password,token,secret,apiKey,access_token,refresh_token` | csv | `password,secret` | Mask JSON fields |

## H) CORS

| ENV | Default | Format | Example | Effect |
|---|---|---|---|---|
| `CORS_ALLOWED_ORIGINS` | `""` | csv / `*` | `https://app.example.com` | CORS allowlist |

---

## Running DEV/PROD

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

Run with env file:

```bash
docker run --rm -p 8080:8080 --env-file .env mcp-swagger-gateway
```

### Example `.env`

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

## PROD: systemd (example)

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

## PROD: Kubernetes (brief)

- readiness/liveness: `GET /healthz`
- secrets (`INBOUND_OAUTH_CLIENT_SECRET`, `UPSTREAM_OAUTH_CLIENT_SECRET`, `UPSTREAM_API_KEY_VALUE`) should be stored in `Secret`
- for write profiles: separate deployment and separate credentials

---

## MCP Resources ✅

## Catalog ✅

- `swagger:endpoints`
- `swagger:endpoints:{METHOD}`
- `swagger:endpointByOperationId:{operationId}`
- `swagger:schema:{name}`
- `swagger:lookup:{pointer}`
- `docs:tool-schemas`

## `swagger:endpoints` ✅

Purpose: return all resolved endpoints.

Example:

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

Example: `swagger:endpoints:GET`

Response: array of `ResolvedOperation` for the specified method only.

## `swagger:endpointByOperationId:{operationId}` ✅

Example: `swagger:endpointByOperationId:getUserById`

Response: a single `ResolvedOperation`.

## `swagger:schema:{name}` ✅

Example: `swagger:schema:User`

Response: schema from `components.schemas.User`.

## `swagger:lookup:{pointer}` ✅

Pointer examples:

- `/components/schemas/User`
- `/paths/~1users~1{id}/get`
- URL encoded: `%2Fcomponents%2Fschemas%2FUser`

Response: any object from the Swagger spec.

## `docs:tool-schemas` ✅

Purpose: formal JSON Schema for MCP tool input/output.

URI: `docs://tool-schemas`

Response:

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

### Endpoint DTO Format

Fields (minimum):

- method/baseURL/pathTemplate/urlTemplate/(optional)exampleURL/operationId
- summary/description/tags/deprecated
- path/query/header/cookie params
- request: contentTypes/bodySchema/examples
- responses.success / responses.errors
- security / servers

### What "nested schemas are resolved" Means ⚠️

- `$ref` is resolved into a JSON object,
- `allOf/oneOf/anyOf` are preserved as structure, but nested `$ref` inside them are resolved,
- cycles are marked with technical markers (e.g., `x-circularRef`).

---

## Tool Schemas ✅

- Canonical source of schemas: `internal/tool/schemas.go`.
- Publication for agents:
  - via resource `docs:tool-schemas` (`docs://tool-schemas`);
  - via `tools/list` (SDK gets `inputSchema` and `outputSchema`).
- Covered tools:
  - `swagger.search`
  - `swagger.plan_call`
  - `swagger.http.generate_payload`
  - `swagger.http.prepare_request`
  - `swagger.http.validate_request`
  - `swagger.http.execute`
  - `swagger.http.validate_response`
  - `policy.request_confirmation`
  - `policy.confirm`

Result schema for swagger/policy tools is unified:

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

Unified JSON contract for all `swagger.*` tools ✅:

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

`operationId` is required for `generate_payload`, `prepare_request`, `validate_request`, `execute`, `validate_response`.
For `search` and `plan_call`, filters can be passed in `params.query`.
⚠️ Legacy format (`pathParams`, `queryParams`, `body`, `headers` at the top level) is supported as a backward-compatible adapter.

Common response format:

```json
{
  "ok": true,
  "data": {},
  "error": null
}
```

Errors:

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

Error codes:

- `plan_only`
- `policy_denied`
- `confirmation_required`
- `invalid_request`
- `network_error`
- `timeout`
- `rate_limited`
- `upstream_error`
- `no_base_url`

For write-flow with `REQUIRE_CONFIRMATION_FOR_WRITE=true`, additional tools are available:

- `policy.request_confirmation`
- `policy.confirm`

## `swagger.search` ✅

Input:

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

Output:

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

New capabilities of `swagger.search`:

- search endpoints by schema usage (`schema: "User"`),
- search endpoints by status code (`status: 404`) with emphasis on error responses,
- `include` controls response sections:
  - `endpoints` — ranked list of operations,
  - `schemas` — aggregates for found schemas,
  - `usage` — usage indexes for schema/status.

If `X-Correlation-Id` (or the header from `CORRELATION_ID_HEADER`) is not passed in `params.headers`, the tool adds it automatically.

## `swagger.plan_call` ✅

Input:

```json
{
  "operationId":"getUserById",
  "params": {
    "query": {"goal":"Get a user"}
  }
}
```

Output:

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

Purpose: generate `params.body` from the request body schema of the selected operation.

Input:

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

Where:

- `strategy`:
  - `minimal` — only required fields;
  - `example` — prefers `example/examples`;
  - `maximal` — tries to fill more fields.
- `seed` — deterministic value generation.
- `overrides` — patch on top of the generated body.

Output:

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

Generator behavior:

- fills required fields;
- accounts for `enum`, `minLength`, `minimum`, `format` (email/uuid/date/date-time/uri/ip);
- supports `object`, `array`, `primitive`;
- for `oneOf/anyOf`, selects a variant deterministically and returns a warning;
- for `allOf`, merges object parts.

## `swagger.http.prepare_request` ✅

Input:

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

Output:

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

Input: same as `prepare_request`.

Output:

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

Input:

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

Output:

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

Response body format (strict):

- `contentType: string`
- `bodyEncoding: "json" | "text" | "base64"`
- `body: any|string`

Decoding rules:

1. If `Content-Type` contains `json` or the payload looks like JSON and parses successfully -> `bodyEncoding=json`
2. Otherwise if the payload is valid UTF-8 text -> `bodyEncoding=text`
3. Otherwise -> `bodyEncoding=base64`

`text` response example:

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

Binary response example:

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

### Execution Order in `execute`

1. Get endpoint by `operationId`
2. Select baseURL (`sandbox` -> `UPSTREAM_SANDBOX_BASE_URL`, then `UPSTREAM_BASE_URL`, then `endpoint.baseURL`/`servers`)
3. Policy evaluate
4. Request validation (if enabled)
5. Apply upstream auth
6. HTTP call via constrained client
7. Response read/decode with limits
8. Response validation (if enabled)
9. Audit log

### `MCP_API_MODE` Modes

- `plan_only`: execute is forbidden
- `execute_readonly`: typically only GET/HEAD/OPTIONS
- `execute_write`: write is allowed per policy
- `sandbox`: forced sandbox base URL

### `confirmation_required`

If `REQUIRE_CONFIRMATION_FOR_WRITE=true`, a write method will return:

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

Then use the confirmation flow:

1. `policy.request_confirmation`
2. `policy.confirm` (approve=`true`)
3. retry `swagger.http.execute` with `confirmationId`

## `swagger.http.validate_response` ✅

Input:

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

Output:

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

`swagger.http.validate_response` uses the same `contentType/bodyEncoding/body` format as `swagger.http.execute`.

## `policy.request_confirmation` ✅

Purpose: create a confirmation for a potentially dangerous write call.

Input:

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

Output:

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

Purpose: approve or reject a previously created confirmation request.

Input:

```json
{
  "confirmationId": "7b30e9f2a4f748f4b1f95d51f56e8f9f",
  "approve": true
}
```

Output:

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

Example (`text`):

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

Example (`base64`):

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

### Semantics of `execute` vs `validate_response`

Adopted behavior (Option A):

- `swagger.http.execute`:
  - performs the actual HTTP call;
  - if `VALIDATE_RESPONSE=true`, performs built-in response validation and puts the result in `data.responseValidation`;
  - **does not fail** due to contract mismatch (i.e., `ok` remains `true`, and mismatches go into `responseValidation.errors`).
- `swagger.http.validate_response`:
  - a separate tool for explicit/repeated validation of an already received response;
  - returns a normalized body (`contentType/bodyEncoding/body`) + validation result (`valid/errors`) and does not perform an HTTP call.

When to use:

- need a real API call + mismatch diagnostics: `swagger.http.execute`;
- need to recheck/compare a response separately (e.g., after post-processing the body): `swagger.http.validate_response`.

---

## MCP Prompt Templates ✅

## `swagger.call_agent` ✅

Purpose: sets up a safe workflow for the agent.

Current workflow in the template (5 steps):

1. Clarify the user's goal and risk.
2. Find operations (`swagger.search`).
3. Plan the call (`swagger.plan_call`).
4. Prepare the request (`swagger.http.prepare_request`).
5. Validate the request (`swagger.http.validate_request`), execute the call (`swagger.http.execute`) and validate the response (`swagger.http.validate_response`).

⚠️ The `swagger.call_agent` template does not insert a separate `policy.request_confirmation`/`policy.confirm` step by default; for write scenarios, the agent must add this flow on its own.

Pseudo-dialog:

```text
Agent -> swagger.search(...)
Agent -> swagger.plan_call(...)
Agent -> swagger.http.prepare_request(...)
Agent -> swagger.http.validate_request(...)
Agent -> policy.request_confirmation(...) [optional for write]
Agent -> policy.confirm(...) [optional for write]
Agent -> swagger.http.execute(...)
Agent -> swagger.http.validate_response(...)
```

---

## Security and Guardrails ✅

### Why Execute Is Dangerous Without Restrictions

Risks:

- data exfiltration,
- mass write operations,
- secret leakage in logs,
- destructive actions without control.

### Production Recommendations

1. Keep `plan_only` as default.
2. For read use-case: `execute_readonly`.
3. For write:
   - `ALLOWED_OPERATION_IDS` is mandatory,
   - `DENIED_METHODS=DELETE` by default,
   - `REQUIRE_CONFIRMATION_FOR_WRITE=true`.
4. Separate inbound and outbound credentials.
5. Enable audit + redaction.
6. Enable rate/concurrency/size limits.
7. For tests, enable `sandbox`.

### Policy Priorities (deterministic order) ✅

Decision order in `internal/policy`:

| Step | Check | Result on trigger |
|---|---|---|
| 1 | `DENIED_OPERATION_IDS` | deny: `policy_denied` |
| 2 | `DENIED_METHODS` | deny: `policy_denied` |
| 3 | `ALLOWED_OPERATION_IDS` (if set) | if `operationId` not in allowlist -> deny |
| 4 | `ALLOWED_METHODS` (if set) | if method not in allowlist -> deny |
| 5 | `MCP_API_MODE` | `plan_only` -> deny `plan_only`; `execute_readonly` -> deny write-methods; `execute_write/sandbox` -> pass |
| 6 | `REQUIRE_CONFIRMATION_FOR_WRITE` | for write-methods -> deny `confirmation_required` |

Notes:

- deny rules always override allow rules.
- allowlist by `operationId` restricts calls to only the listed operations.
- `execute_readonly` additionally restricts methods to `GET/HEAD/OPTIONS`, even if `ALLOWED_METHODS` is broader.

Examples:

1. `DENIED_OPERATION_IDS=createUser`, `ALLOWED_OPERATION_IDS=createUser`, method=`POST`
   result: deny at step 1 (`operationId explicitly denied`).
2. `DENIED_METHODS=POST`, `ALLOWED_METHODS=POST`, mode=`execute_write`
   result: deny at step 2 (`HTTP method explicitly denied`).
3. mode=`execute_readonly`, `ALLOWED_METHODS=POST`, method=`POST`
   result: deny at step 5 (readonly mode blocks write).
4. mode=`execute_write`, `REQUIRE_CONFIRMATION_FOR_WRITE=true`, method=`POST`
   result: deny at step 6 (`confirmation_required`).

### Mini Threat Model

- Prompt injection via Swagger descriptions
- SSRF via base URL override
- Credential leakage
- Mass-write

Countermeasures: policy, auth split, allow/deny, sandbox, audit/redaction, limits.

### SSRF Protection and Host Allowlist ✅

Two levels of protection are implemented:

1. **Fail-fast at startup**:
   - `UPSTREAM_BASE_URL`, `UPSTREAM_SANDBOX_BASE_URL`, `SWAGGER_BASE_URL` are validated,
   - if `SWAGGER_PATH` is a URL — the host is validated,
   - after loading Swagger, `servers`/`baseURL` of all operations are validated.
2. **Defense-in-depth before each `swagger.http.execute`**:
   - the selected `baseURL` is re-validated,
   - the final `finalURL` is validated,
   - redirects are allowed only if the target URL passes the same host policy check.
3. **Safe Swagger loading via URL**:
   - only `http/https` are supported,
   - redirects are limited (`max 5`) and each hop is validated through policy,
   - `SWAGGER_HTTP_TIMEOUT` and `SWAGGER_MAX_BYTES` are applied.

Production configuration example:

```bash
SWAGGER_PATH=https://specs.example.com/openapi.yaml
SWAGGER_ALLOWED_HOSTS=specs.example.com

UPSTREAM_BASE_URL=https://api.example.com
UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
UPSTREAM_ALLOWED_HOSTS=api.example.com,staging-api.example.com
BLOCK_PRIVATE_NETWORKS=true
```

Recommendations:

- set explicit allowlists for Swagger and upstream separately,
- do not use wildcard `*` for host allowlist,
- keep `BLOCK_PRIVATE_NETWORKS=true` in production.

---

## Observability

What is logged:

- ✅ service logs (`slog`)
- ✅ audit records for execute

Audit event example:

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

- ✅ streamable middleware generates a correlation id (UUID) if the incoming header is absent
- ✅ correlation id is placed in context and returned in the HTTP response header
- ✅ `swagger.http.execute` propagates the correlation id to the upstream request header (if `params.headers` does not contain it)
- ✅ the audit event includes the `correlationId` field
- ✅ in stdio mode, `swagger.http.execute` generates a correlation id for each call if not explicitly provided

Metrics:

- ✅ endpoint: `GET /metrics`
- ✅ metrics:
  - `mcp_execute_total{operationId,method,status}`
  - `mcp_execute_errors_total{code}`
  - `mcp_execute_duration_seconds_bucket` (histogram buckets)
  - `mcp_execute_inflight`
  - `mcp_rate_limited_total`

Request example:

```bash
curl -s http://127.0.0.1:8080/metrics | head -n 40
```

If `METRICS_AUTH_REQUIRED=true`:

```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8080/metrics | head -n 40
```

Prometheus scrape config example:

```yaml
scrape_configs:
  - job_name: mcp-swagger-gateway
    metrics_path: /metrics
    static_configs:
      - targets: ["mcp-swagger-gateway:8080"]
```

⚠️ When `METRICS_AUTH_REQUIRED=false`, expose `/metrics` only on an internal/private network.

---

## Roadmap

- 🧭 Expand integration tests for streamable (`GET /mcp`, `DELETE /mcp`, CORS preflight scenarios).
- 🧭 Deeper normalization of OpenAPI compositions (`allOf/oneOf/anyOf`) with an optional merge mode.

---

## FAQ / Troubleshooting

## Swagger Does Not Parse

```bash
ls -la ./openapi.yaml
curl -I https://example.com/openapi.yaml
export SWAGGER_FORMAT=yaml
```

Check source type:

- local file: `SWAGGER_PATH=./openapi.yaml`;
- URL: only `http://`/`https://` (`file://` is rejected).

If `SWAGGER_PATH` is a URL:

- check `SWAGGER_ALLOWED_HOSTS`,
- check `BLOCK_PRIVATE_NETWORKS`,
- check `SWAGGER_HTTP_TIMEOUT`,
- check `SWAGGER_MAX_BYTES` (if the spec is large).

## `no_base_url`

```bash
export UPSTREAM_BASE_URL=https://api.example.com
# or for sandbox
export MCP_API_MODE=sandbox
export UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
```

## 401/403 on `/mcp`

Check inbound OAuth:

- `INBOUND_OAUTH_*` parameters
- issuer/audience/scopes
- bearer token validity

## 401/403 on Upstream API

Check outbound auth:

- `UPSTREAM_AUTH_MODE`
- `UPSTREAM_OAUTH_*` or `UPSTREAM_API_KEY_*` / `UPSTREAM_BEARER_TOKEN`

## Response Validation Errors

`execute` may succeed, but `responseValidation.valid=false`.

This signals a desynchronization between Swagger and the real API.

## Timeout / Response Too Large

- increase `HTTP_TIMEOUT`
- increase `MAX_RESPONSE_BYTES`
- check the response size of the endpoint

## `policy_denied`: host blocked by security policy

Cause: URL/host does not pass SSRF guardrails.

Check:

- `UPSTREAM_ALLOWED_HOSTS` (for `execute`) and/or `SWAGGER_ALLOWED_HOSTS` (for `SWAGGER_PATH` URL),
- `BLOCK_PRIVATE_NETWORKS=true` blocks `127.0.0.1`, RFC1918 and link-local networks,
- the target API redirect leads to an allowed host.

## Errors Loading `SWAGGER_PATH` via URL

Common causes:

- `unsupported swagger URL scheme`: a scheme other than `http/https` was specified (e.g., `file://`);
- `swagger url blocked by policy`: host does not pass `SWAGGER_ALLOWED_HOSTS` / private-network policy;
- `swagger redirect blocked by policy`: redirect target does not pass the same host policy;
- `too many redirects while fetching swagger`: redirect hop limit (5) exceeded;
- `swagger payload exceeds configured size limit`: `SWAGGER_MAX_BYTES` exceeded.

## `confirmation_required`

This is a guardrail for write operations.

Safe path:

1. Call `policy.request_confirmation`.
2. Get human confirmation and call `policy.confirm` with `approve=true`.
3. Retry `swagger.http.execute`, passing `confirmationId`.

---

## Hands-on Scenarios

## 1) Find Endpoint -> Prepare -> Validate -> Execute GET

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

## 2) Write Endpoint -> confirmation_required -> confirm -> execute

```bash
export MCP_API_MODE=execute_write
export REQUIRE_CONFIRMATION_FOR_WRITE=true
export CONFIRMATION_TTL=10m
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"createUser","params":{"body":{"email":"new@example.com"}}}}
```

Expected:

```json
{"ok":false,"data":null,"error":{"code":"confirmation_required"}}
```

Create a confirmation request:

```json
{"tool":"policy.request_confirmation","arguments":{"operationId":"createUser","reason":"method \"POST\" requires explicit user confirmation","preparedRequestSummary":{"operationId":"createUser","method":"POST","finalURL":"https://api.example.com/users"}}}
```

Confirm:

```json
{"tool":"policy.confirm","arguments":{"confirmationId":"<id-from-previous-step>","approve":true}}
```

Retry execute with `confirmationId`:

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"createUser","confirmationId":"<id-from-previous-step>","params":{"body":{"email":"new@example.com"}}}}
```

## 3) Sandbox Mode

```bash
export MCP_API_MODE=sandbox
export UPSTREAM_SANDBOX_BASE_URL=https://staging-api.example.com
```

```json
{"tool":"swagger.http.execute","arguments":{"operationId":"getUserById","params":{"path":{"id":"123"}}}}
```

The URL in the response should be the sandbox one.

## 4) Find Schema and Build Payload

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

Shortened response example:

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

## Commands

```bash
make tidy
make fmt
make lint
make test
make build
make run
```
