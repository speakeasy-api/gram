# MCP Registry Reference

This document describes how Gram integrates with MCP registries to discover and connect to external MCP servers.

---

## What is an MCP Registry?

An MCP registry is a catalog service providing two essential functions:

1. **Discovery**: Search and browse available MCP servers by name, description, or capability
2. **Installation metadata**: Connection details needed to use a server:
   - Remote URLs and transport types (streamable-http, sse)
   - Authentication requirements (OAuth, API keys, headers)
   - Available tools and their schemas

Registries conform to the [MCP Registry Spec](https://registry.modelcontextprotocol.io/docs).

---

## PulseMCP: Our Registry Provider

Gram uses [PulseMCP](https://pulsemcp.com) as our registry provider. PulseMCP implements the MCP registry spec and enriches it with metadata that would otherwise require connecting to each server individually.

### Progressive Enhancement

Gram progressively enhances to PulseMCP metadata when available:

| Data | Base Registry | PulseMCP Enrichment |
|------|---------------|---------------------|
| Server listing | Yes | Yes |
| Remote URLs & transport | Yes | Yes |
| **Tool lists** | No* | Yes |
| **Auth requirements** | Partial | Full (authOptions) |
| Usage metrics | No | Yes (visitor estimates) |
| Official status | No | Yes (isOfficial flag) |

*\*Tool lists normally require an authenticated connection to the MCP server. PulseMCP pre-indexes tools, solving a key challenge in Gram's architecture.*

### Tool Lists: Solving the Bootstrap Problem

Without PulseMCP, Gram cannot know what tools a server provides until after authentication. This creates a bootstrap problem: users need to see available tools before deciding to connect.

PulseMCP solves this by:
1. Connecting to servers and indexing their tools
2. Publishing tool metadata in the registry response
3. Keeping tool lists updated as servers evolve

**Gram's fallback**: When tool lists aren't available (e.g., new servers not yet indexed), Gram uses proxy tools that defer capability discovery until runtime.

---

## PulseMCP Metadata Structure

PulseMCP metadata lives in the `_meta` field at two levels:

### Server-level (`_meta["com.pulsemcp/server"]`)

```json
{
  "isOfficial": true,
  "visitorsEstimateMostRecentWeek": 12500,
  "visitorsEstimateLastFourWeeks": 54307,
  "visitorsEstimateTotal": 198000
}
```

### Version-level (`_meta["com.pulsemcp/server-version"]`)

```json
{
  "source": "registry.modelcontextprotocol.io",
  "status": "active",
  "isLatest": true,
  "publishedAt": "2025-01-15T00:00:00Z",
  "updatedAt": "2025-01-20T00:00:00Z",
  "remotes[0]": {
    "auth": { ... },
    "authOptions": [ ... ],
    "tools": [ ... ]
  }
}
```

Note: The `remotes[0]` key is a literal string, not array access.

---

## Authentication Options (authOptions)

The `authOptions` array describes how to authenticate with a server. Servers may support multiple auth methods.

### Auth Types

| Type | Description |
|------|-------------|
| `open` | No authentication required |
| `api_key` | Header-based API key or token |
| `oauth` | OAuth 2.0 flow with authorization server |

### API Key Example

```json
{
  "type": "api_key",
  "detail": {
    "sources": [
      { "name": "Authorization", "location": "header" },
      { "name": "x-custom-header", "location": "header" }
    ]
  }
}
```

### OAuth Example

```json
{
  "type": "oauth",
  "detail": {
    "protectedResourceMetadata": {
      "resource": "https://mcp.stripe.com",
      "authorization_servers": ["https://access.stripe.com/mcp"]
    },
    "authorizationServerMetadata": {
      "issuer": "https://access.stripe.com/mcp",
      "token_endpoint": "https://access.stripe.com/mcp/oauth2/token",
      "authorization_endpoint": "https://access.stripe.com/mcp/oauth2/authorize",
      "scopes_supported": ["mcp"],
      "grant_types_supported": ["authorization_code", "refresh_token"],
      "code_challenge_methods_supported": ["S256"]
    },
    "grantTypes": {
      "humanToMachine": { "authorizationCode": true },
      "machineToMachine": { "jwtBearer": false, "clientCredentials": false }
    },
    "clientRegistration": {
      "dynamic": {
        "supported": true,
        "redirectsAccepted": { "localhost": true, "custom": false }
      }
    }
  }
}
```

### Dual Auth Support

Some servers (e.g., Stripe, GitHub) support both OAuth and API key authentication:

```json
{
  "authOptions": [
    { "type": "oauth", "detail": { ... } },
    { "type": "api_key", "detail": { "sources": [{ "name": "Authorization", "location": "header" }] } }
  ]
}
```

See issue `gram-uwr` for graceful handling of dual-auth servers.

---

## API Reference

Base URL: `https://api.pulsemcp.com`

### Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /v0.1/servers` | List servers with search/filter |
| `GET /v0.1/servers/{name}/versions/latest` | Get server details |
| `GET /v0.1/servers/{name}/versions` | List all versions |

### Authentication

```bash
-H "X-API-Key: $PULSE_REGISTRY_KEY"
-H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"
```

### Query Parameters

| Parameter | Description |
|-----------|-------------|
| `search` | Full-text search query |
| `limit` | Results per page (1-100, default 30) |
| `cursor` | Pagination cursor from previous response |
| `version` | Set to `latest` for current versions only |
| `updated_since` | RFC3339 timestamp for incremental sync |

---

## CLI Examples

**Note for LLM users**: Add `&limit=N` or pipe through `jq '.servers[:N]'` to limit output size.

All examples assume mise environment is loaded:

```bash
# Shorthand for all examples below
alias pulse='mise exec -- sh -c '\''curl -s "https://api.pulsemcp.com/v0.1/servers$1" -H "X-API-Key: $PULSE_REGISTRY_KEY" -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"'\'''
```

### List servers

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&limit=10" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '.servers[].server.name'
```

### Search for servers

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&search=github" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '.servers[] | {name: .server.name, description: .server.description}'
```

### Get server connection info

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&search=stripe" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '.servers[0] | {
      name: .server.name,
      url: .server.remotes[0].url,
      type: .server.remotes[0].type,
      auth_types: [._meta["com.pulsemcp/server-version"]["remotes[0]"].authOptions[]?.type]
    }'
```

Output:
```json
{
  "name": "com.stripe/mcp",
  "url": "https://mcp.stripe.com",
  "type": "streamable-http",
  "auth_types": ["oauth", "api_key"]
}
```

### List servers with their tools

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&limit=50" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '.servers[]
      | select(._meta["com.pulsemcp/server-version"]["remotes[0]"].tools | length > 0)
      | {
          server: .server.name,
          tools: [._meta["com.pulsemcp/server-version"]["remotes[0]"].tools[].name]
        }'
```

Output:
```json
{
  "server": "app.linear/linear",
  "tools": ["list_comments", "create_comment", "list_cycles", "get_document", "list_documents", "create_document", "update_document", "get_issue", "list_issues", "create_issue", "update_issue", "list_issue_statuses", "get_issue_status", "list_issue_labels", "create_issue_label", "list_projects", "get_project", "create_project", "update_project", "list_project_labels", "list_teams", "get_team", "list_users", "get_user", "search_documentation"]
}
{
  "server": "com.stripe/mcp",
  "tools": ["search_stripe_documentation", "get_stripe_account_info", "create_customer", ...]
}
```

### Filter official servers by popularity

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&limit=100" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '[.servers[]
      | select(._meta["com.pulsemcp/server"].isOfficial == true)
      | {
          name: .server.name,
          visitors: ._meta["com.pulsemcp/server"].visitorsEstimateLastFourWeeks
        }]
      | sort_by(-.visitors)
      | .[:10]'
```

### Find servers supporting OAuth

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&limit=100" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '[.servers[]
      | select(._meta["com.pulsemcp/server-version"]["remotes[0]"].authOptions[]?.type == "oauth")
      | .server.name]
      | unique'
```

### Find servers with both OAuth and API key auth

```bash
mise exec -- sh -c 'curl -s "https://api.pulsemcp.com/v0.1/servers?version=latest&limit=100" \
  -H "X-API-Key: $PULSE_REGISTRY_KEY" \
  -H "X-Tenant-ID: $PULSE_REGISTRY_TENANT"' \
  | jq '[.servers[]
      | select(
          (._meta["com.pulsemcp/server-version"]["remotes[0]"].authOptions // [] | map(.type) | contains(["oauth"])) and
          (._meta["com.pulsemcp/server-version"]["remotes[0]"].authOptions // [] | map(.type) | contains(["api_key"]))
        )
      | .server.name]'
```

Output: `["com.stripe/mcp", "io.github.github/github-mcp-server"]`

### Paginate through all results

```bash
cursor=""
while true; do
  response=$(mise exec -- sh -c "curl -s 'https://api.pulsemcp.com/v0.1/servers?version=latest&limit=100&cursor=$cursor' \
    -H 'X-API-Key: \$PULSE_REGISTRY_KEY' \
    -H 'X-Tenant-ID: \$PULSE_REGISTRY_TENANT'")

  echo "$response" | jq '.servers[].server.name'

  cursor=$(echo "$response" | jq -r '.metadata.nextCursor // empty')
  [[ -z "$cursor" ]] && break
done
```

---

## Response Structure

### Full Server Response Example

```json
{
  "servers": [
    {
      "server": {
        "name": "com.stripe/mcp",
        "description": "MCP server integrating with Stripe - tools for customers, products, payments, and more.",
        "version": "1.0.0",
        "title": null,
        "websiteUrl": "https://stripe.com",
        "icons": [{ "src": "https://..." }],
        "remotes": [
          {
            "url": "https://mcp.stripe.com",
            "type": "streamable-http"
          }
        ]
      },
      "_meta": {
        "com.pulsemcp/server": {
          "isOfficial": true,
          "visitorsEstimateLastFourWeeks": 54307
        },
        "com.pulsemcp/server-version": {
          "source": "registry.modelcontextprotocol.io",
          "status": "active",
          "isLatest": true,
          "remotes[0]": {
            "auth": { "type": "oauth", "detail": { ... } },
            "authOptions": [
              { "type": "oauth", "detail": { ... } },
              { "type": "api_key", "detail": { ... } }
            ],
            "tools": [
              {
                "name": "search_stripe_documentation",
                "description": "Search Stripe documentation...",
                "inputSchema": { "type": "object", "properties": { ... } }
              }
            ]
          }
        }
      }
    }
  ],
  "metadata": {
    "count": 150,
    "nextCursor": "eyJvZmZzZXQiOjMwfQ=="
  }
}
```

---

## Gram's Registry Client

Location: `server/internal/externalmcp/registryclient.go`

### Key Types

| Go Type | Purpose |
|---------|---------|
| `RegistryClient` | HTTP client for registry communication |
| `Registry` | Registry endpoint config (ID, URL) |
| `ListServersParams` | Query params (Search, Cursor) |
| `ServerDetails` | Parsed server info with remote URL and transport type |

### Transport Type Priority

When multiple remotes are available, Gram prefers:
1. `streamable-http` (preferred)
2. `sse` (fallback)

