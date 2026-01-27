# gram-u3e: Consolidate header merging logic in externalmcp/config.go

**Type**: bug + refactor
**Priority**: P2

## Motivation

We need to forward headers from system + user configuration to external MCPs for **both**:
1. **Proxy tools** - runtime tool discovery via `ProxyToolExecutor`
2. **Materialized tools** - pre-defined tools with known schemas

The header merging logic must be identical for both, so we need a shared implementation with a clean public interface.

## The Bug

**Current behavior in `ProxyToolExecutor.connect()`:**
```go
for _, def := range e.plan.HeaderDefinitions {
    value := env.SystemEnv.Get(def.Name)
    // ... lookup and override logic
    headers[def.HeaderName] = value
}
```

This **filters** system env by header definitions. Only env vars that have a matching header definition are considered.

**Correct behavior (matching `doFunction` pattern):**
```go
// 1. Start with ALL system env values as potential headers
for k, v := range env.SystemEnv.All() {
    headerName := deriveHeaderName(k, headerDefs)
    headers[headerName] = v
}

// 2. User config can override values for keys with header definitions
for _, def := range headerDefs {
    if val := env.UserConfig.Get(def.Name); val != "" {
        headers[def.HeaderName] = val
    }
}
```

System env values flow through **regardless of whether a header definition exists**.

## Header Name Derivation

When a system env var doesn't have an explicit header definition:
- Use `toolconfig.ToHTTPHeader(envVarName)` to convert env var name to HTTP header format
- Example: `SLACK_X_API_KEY` → `Slack-X-Api-Key`

When a header definition exists:
- Use the definition's `HeaderName` field
- Example: `SLACK_X_API_KEY` with definition `{HeaderName: "X-Api-Key"}` → `X-Api-Key`

## Implementation

### 1. Create `externalmcp/config.go`

```go
package externalmcp

import "github.com/speakeasy-api/gram/server/internal/toolconfig"

// BuildHeaders constructs HTTP headers from system environment and user configuration.
//
// Logic:
// 1. All system env values become headers (using ToHTTPHeader for name derivation)
// 2. Header definitions provide custom name mappings (override default derivation)
// 3. User config can override values for keys that have header definitions
func BuildHeaders(
    systemEnv *toolconfig.CaseInsensitiveEnv,
    userConfig *toolconfig.CaseInsensitiveEnv,
    headerDefs []HeaderDefinition,
) map[string]string {
    // Build lookup map: env var name -> header definition
    defsByEnvName := make(map[string]HeaderDefinition)
    for _, def := range headerDefs {
        defsByEnvName[strings.ToLower(def.Name)] = def
    }

    headers := make(map[string]string)

    // 1. All system env values become headers
    for envKey, value := range systemEnv.All() {
        var headerName string
        if def, ok := defsByEnvName[envKey]; ok {
            headerName = def.HeaderName
        } else {
            headerName = toolconfig.ToHTTPHeader(envKey)
        }
        headers[headerName] = value
    }

    // 2. User config overrides for keys with header definitions
    for _, def := range headerDefs {
        if val := userConfig.Get(def.Name); val != "" {
            headers[def.HeaderName] = val
        }
    }

    return headers
}
```

### 2. Update `ProxyToolExecutor.connect()`

```go
func (e *ProxyToolExecutor) connect(ctx context.Context, env toolconfig.ToolCallEnv) (*Client, error) {
    opts := &ClientOptions{
        Authorization: "",
        Headers:       BuildHeaders(env.SystemEnv, env.UserConfig, e.plan.HeaderDefinitions),
    }
    return NewClient(ctx, e.logger, e.plan.RemoteURL, e.plan.TransportType, opts)
}
```

### 3. Use in materialized tool execution

The same `BuildHeaders` function will be used when implementing materialized external MCP tool calls.

## Files Changed

| File | Change |
|------|--------|
| `server/internal/externalmcp/config.go` | NEW - BuildHeaders function |
| `server/internal/externalmcp/config_test.go` | NEW - Tests |
| `server/internal/externalmcp/proxytoolexecutor.go` | UPDATE - Use BuildHeaders |

## Test Cases

1. **All system env flows through** - system env with no header defs still produces headers
2. **Header definition renames** - env var with definition uses definition's HeaderName
3. **User config overrides** - user value replaces system value for defined keys
4. **User config only for defined keys** - user config ignored for keys without definitions
5. **Case insensitivity** - lookups work regardless of key casing
