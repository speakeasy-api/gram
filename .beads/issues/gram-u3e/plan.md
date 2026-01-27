# Execution Plan: gram-u3e - Consolidate Header Merging Logic

## Problem Summary

The `ProxyToolExecutor.connect()` method has incorrect header merging logic:

- **Current (wrong)**: Iterates over header definitions only, looks up system env values for each defined key
- **Correct**: The system environment should be the base layer, with user config overlaying for keys defined in header definitions

The subtle bug: if system env has a value under a different key format than the header definition expects, it might be missed.

## Comparison with doFunction (correct reference)

```go
// doFunction does this correctly:
payloadEnv := make(map[string]string)

// 1. Start with ALL system env vars
for k, v := range env.SystemEnv.All() {
    payloadEnv[strings.ToUpper(k)] = v
}

// 2. For each required variable, allow user config to override
for key := range plan.Variables {
    if val := env.UserConfig.Get(key); val != "" {
        payloadEnv[key] = val
    }
}
```

## Implementation Steps

### Step 1: Create `externalmcp/config.go`

Create a new file with a `MergeHeaders` function:

```go
package externalmcp

import "github.com/speakeasy-api/gram/server/internal/toolconfig"

// MergeHeaders merges system environment and user config into HTTP headers.
//
// Logic:
// 1. For each header definition, look up the env key in system env
// 2. If user config has a value for that key, it overrides system env
// 3. Return map of header name -> resolved value
func MergeHeaders(
    systemEnv *toolconfig.CaseInsensitiveEnv,
    userConfig *toolconfig.CaseInsensitiveEnv,
    headerDefs []HeaderDefinition,
) map[string]string {
    headers := make(map[string]string)

    for _, def := range headerDefs {
        // Get value from system env (case-insensitive lookup)
        value := systemEnv.Get(def.Name)

        // Also try normalized form
        normalizedName := toolconfig.ToNormalizedEnvKey(def.Name)
        if value == "" {
            value = systemEnv.Get(normalizedName)
        }

        // User config can override
        if userValue := userConfig.Get(def.Name); userValue != "" {
            value = userValue
        } else if userValue := userConfig.Get(normalizedName); userValue != "" {
            value = userValue
        }

        if value != "" {
            headers[def.HeaderName] = value
        }
    }

    return headers
}
```

### Step 2: Update `ProxyToolExecutor.connect()`

Replace the inline header merging logic with a call to `MergeHeaders`:

```go
func (e *ProxyToolExecutor) connect(ctx context.Context, env toolconfig.ToolCallEnv) (*Client, error) {
    opts := &ClientOptions{
        Authorization: "",
        Headers:       MergeHeaders(env.SystemEnv, env.UserConfig, e.plan.HeaderDefinitions),
    }

    return NewClient(ctx, e.logger, e.plan.RemoteURL, e.plan.TransportType, opts)
}
```

### Step 3: Verify gateway integration

`gateway/proxy.go:doExternalMCP()` calls `ProxyToolExecutor.DoCall()` which uses `connect()`. No changes needed there - it automatically benefits from the fix.

### Step 4: Add tests

Create `externalmcp/config_test.go` with test cases:

1. **Basic merge** - system env value flows through to header
2. **User override** - user config value takes precedence over system env
3. **Normalized key lookup** - header def with `SLACK_X_API_KEY` finds `slack_x_api_key` in env
4. **Empty value skipped** - no header set when both system and user are empty
5. **Multiple headers** - multiple header definitions all resolve correctly

## Files Changed

| File | Change |
|------|--------|
| `server/internal/externalmcp/config.go` | NEW - MergeHeaders function |
| `server/internal/externalmcp/config_test.go` | NEW - Tests for MergeHeaders |
| `server/internal/externalmcp/proxytoolexecutor.go` | UPDATE - Use MergeHeaders in connect() |

## Verification

1. Run `mise lint:server` to check for errors
2. Run `go test ./server/internal/externalmcp/...` to verify tests pass
3. Manually verify the logic matches `doFunction` pattern
