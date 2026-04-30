---
name: mcp-spec
description: Lookup map for the Model Context Protocol (MCP) specification — pinned URLs to spec sections most relevant to Gram's MCP pathway: transports (Streamable HTTP, stdio, deprecated SSE), the OAuth 2.1 authorization handshake (protected-resource and authorization-server discovery), MCP registries, the Inspector, and core primitives. Activate when a task touches the MCP wire protocol, MCP handshakes, MCP-over-SSE, MCP OAuth discovery (`/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`), MCP registries, or the MCP Inspector. NOT for general Gram architecture work — use the `gram` skill for that.
---

This skill is an index, not an explainer. It pins authoritative URLs for the parts of the MCP spec that the Gram MCP pathway interacts with so agents can fetch the current rules instead of relying on stale memory. The spec is dated and revs frequently — always confirm the version before quoting requirements.

## Spec home

- Spec entry point (current version always linked from here): https://modelcontextprotocol.io/specification/
- Current spec version at time of writing: **2025-11-25**.
- Versioned root: https://modelcontextprotocol.io/specification/2025-11-25/
- Spec + schema repo: https://github.com/modelcontextprotocol/modelcontextprotocol
- TypeScript source-of-truth schema: https://github.com/modelcontextprotocol/specification/blob/main/schema/2025-11-25/schema.ts
- Generated JSON Schema: https://github.com/modelcontextprotocol/specification/blob/main/schema/2025-11-25/schema.json
- Doc index for finding any page: https://modelcontextprotocol.io/llms.txt

## Base protocol and lifecycle

- Base protocol (JSON-RPC 2.0, message shapes, `_meta`, icons): https://modelcontextprotocol.io/specification/2025-11-25/basic
- Lifecycle (`initialize` / `notifications/initialized`, capability negotiation, version negotiation, shutdown): https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle
- The `MCP-Protocol-Version` HTTP header is required on every subsequent HTTP request after init; servers without it assume `2025-03-26`.

## Transports

Spec page: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports

- **Streamable HTTP** (current default for HTTP). Single MCP endpoint serving `POST` (client → server messages) and optional `GET` (open SSE stream for server-initiated traffic). Sessions identified by `MCP-Session-Id` header set on `InitializeResult`. Gotcha: clients **MUST** send `Accept: application/json, text/event-stream` on every POST — the server picks per-response which content type to use.
- **stdio**. Newline-delimited JSON-RPC over a child process's stdin/stdout; stderr is logging only. Gotcha: `stdout` MUST contain only valid MCP messages — any stray print breaks framing.
- **HTTP+SSE (deprecated, protocol 2024-11-05)**. Two-endpoint design (`GET` SSE stream + separate `POST` endpoint advertised via the first `endpoint` SSE event). Replaced by Streamable HTTP. Backwards-compat probe: `POST` an `InitializeRequest`; on 4xx fall back to `GET` and wait for the `endpoint` event. Section: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#backwards-compatibility (old spec lives at https://modelcontextprotocol.io/specification/2024-11-05/basic/transports#http-with-sse).
- Streamable HTTP security warning (Origin validation, localhost binding, DNS rebinding): https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning

## OAuth / authorization

Spec page: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization

Built on OAuth 2.1 + RFC 9728 (Protected Resource Metadata) + RFC 8414 (AS Metadata) + RFC 8707 (Resource Indicators) + Client ID Metadata Documents.

- **Protected resource metadata discovery**: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#protected-resource-metadata-discovery-requirements
  - Servers MUST advertise via either `WWW-Authenticate: Bearer resource_metadata="..."` on a 401, or a well-known URI. Two well-known forms:
    - Path-suffixed: `https://example.com/.well-known/oauth-protected-resource/<mcp-path>`
    - Root: `https://example.com/.well-known/oauth-protected-resource`
  - Clients MUST support both and try `WWW-Authenticate` first, then fall back to well-known probing.
- **Authorization server discovery**: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#authorization-server-metadata-discovery
  - Clients MUST try (in order) for path-bearing issuers: `/.well-known/oauth-authorization-server/<path>`, `/.well-known/openid-configuration/<path>`, `<path>/.well-known/openid-configuration`.
  - For root issuers: `/.well-known/oauth-authorization-server`, then `/.well-known/openid-configuration`.
- **Full handshake sequence diagram**: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#authorization-flow-steps
- **Client registration approaches** (Client ID Metadata Document preferred, then pre-reg, then DCR/RFC 7591): https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#client-registration-approaches
- **Resource Indicators (RFC 8707) — `resource` parameter**: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#resource-parameter-implementation. Clients MUST include `resource=<canonical MCP server URI>` on both authorization and token requests; servers MUST validate token audience.
- **Scope challenge / step-up auth** (`401` initial, `403 insufficient_scope` runtime): https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#scope-challenge-handling
- **PKCE is mandatory** (S256). Clients MUST refuse to proceed if `code_challenge_methods_supported` is absent from AS metadata.
- Auth extensions repo (client_credentials, enterprise IdP, etc.): https://github.com/modelcontextprotocol/ext-auth

## MCP registry

The official metadata index for publicly accessible MCP servers — server metadata only, not packages. Aggregators consume it; host applications consume aggregators.

- Overview: https://modelcontextprotocol.io/registry/about
- Quickstart (publishing): https://modelcontextprotocol.io/registry/quickstart
- Authentication (DNS / HTTP / GitHub OAuth namespace verification): https://modelcontextprotocol.io/registry/authentication
- Remote servers: https://modelcontextprotocol.io/registry/remote-servers
- Package types: https://modelcontextprotocol.io/registry/package-types
- Building aggregators: https://modelcontextprotocol.io/registry/registry-aggregators
- Reference implementation + OpenAPI: https://github.com/modelcontextprotocol/registry
- `server.json` schema: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/server.schema.json
- Registry OpenAPI (the interface other registries can implement): https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/openapi.yaml
- Names use reverse-DNS namespacing (e.g. `io.github.user/server-name`); private servers are out of scope and should live in self-hosted registries.

## Server primitives

Overview: https://modelcontextprotocol.io/specification/2025-11-25/server

- Tools (model-controlled): https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- Resources (application-controlled): https://modelcontextprotocol.io/specification/2025-11-25/server/resources
- Prompts (user-controlled): https://modelcontextprotocol.io/specification/2025-11-25/server/prompts

## Inspector and tooling

- MCP Inspector docs: https://modelcontextprotocol.io/docs/tools/inspector
- Inspector source: https://github.com/modelcontextprotocol/inspector
- Run via `npx @modelcontextprotocol/inspector <command>` — supports stdio and HTTP transports for ad-hoc handshake / tool / resource testing.
- Debugging guide: https://modelcontextprotocol.io/docs/tools/debugging
