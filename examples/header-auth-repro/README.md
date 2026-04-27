# Header Auth Repro

This example is a minimal local API for testing a Livestorm-shaped OpenAPI
integration.

The important behavior now matches Livestorm more closely:

- OpenAPI `info`, `tags`, and security naming follow Livestorm's public spec
- auth is `type: apiKey`, `in: header`, `name: Authorization`
- the OpenAPI document publishes a `/v1` server base by default
- resource paths use Livestorm-style routes such as `/v1/ping`, `/v1/events`,
  `/v1/sessions`, and `/v1/sessions/{id}/people`

It exposes:

- `GET /openapi.json` and `GET /openapi.yaml`: Livestorm-shaped OpenAPI docs
- `GET /v1/ping`: protected auth check
- `GET /v1/me`, `GET /v1/organization`, `GET /v1/events`,
  `GET /v1/events/{id}`, `GET /v1/sessions`, `GET /v1/sessions/{id}`,
  `GET /v1/sessions/{id}/people`: simple local endpoints with sample payloads
- `GET /__debug/headers`: unprotected request header dump for debugging

## Run it

```bash
cd examples/header-auth-repro
EXPECTED_API_KEY=test-secret pnpm start
```

By default the server listens on `http://127.0.0.1:8788`.

By default the OpenAPI document includes:

```yaml
servers:
  - url: https://gram-tiago.ngrok.app/v1
```

This is intended for an ngrok setup like:

```text
https://gram-tiago.ngrok.app -> https://127.0.0.1:8788
```

If you want to force a different public URL, set `PUBLIC_BASE_URL` or
`OPENAPI_SERVER_URL`.

## Quick curl check

Without the header:

```bash
curl -i http://127.0.0.1:8788/v1/ping
```

With the raw `Authorization` header:

```bash
curl -i \
  -H 'Authorization: test-secret' \
  http://127.0.0.1:8788/v1/ping
```

## MCP remote config shape

Point your MCP/OpenAPI tooling at:

```text
https://gram-tiago.ngrok.app/openapi.json
```

For `mcp-remote`, the raw header form looks like:

```json
{
  "command": "npx",
  "args": [
    "mcp-remote@0.1.25",
    "http://127.0.0.1:8788/openapi.json",
    "--header",
    "Authorization:${LIVESTORM_API_KEY}"
  ],
  "env": {
    "LIVESTORM_API_KEY": "test-secret"
  }
}
```

If the header is passed correctly, `/v1/ping` returns `200`. If it is missing or
wrong, the API returns `401` with a JSON body containing both a Livestorm-like
error payload and debug information showing what `Authorization` value was
received.
