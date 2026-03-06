---
name: write-gram-function
description: >-
  Use when authoring Gram Functions with the @gram-ai/functions SDK. Triggers on
  "write gram function", "create gram function", "@gram-ai/functions", "Gram.tool",
  "gf build", "gram function sdk".
license: Apache-2.0
---

# Write Gram Functions

Guide for authoring serverless tools using the `@gram-ai/functions` TypeScript SDK.

## When to Use

- Creating a new Gram Functions project
- Adding tools to an existing Gram Functions project
- Understanding the `Gram`, `ToolContext`, and `assert` APIs
- Configuring environment variables for functions

## Prerequisites

- Node.js >= 22.18.0
- pnpm (or npm/yarn)
- Gram CLI installed for deployment

## Inputs

| Input | Description | Required |
|---|---|---|
| Tool name | Unique identifier for the tool | Yes |
| Tool description | What the tool does (LLM-visible) | Recommended |
| Input schema | Zod schema defining parameters | Yes |
| Execute function | Async handler `(ctx, input) => Response` | Yes |

## Outputs

| Output | Description |
|---|---|
| `dist.zip` | Built zip ready for deployment |
| `manifest.json` | Auto-generated tool manifest (inside zip) |

## Scaffold a New Project

```bash
pnpm create @gram-ai/function@latest --template gram
```

This creates a project with:
- `src/gram.ts` — Main entry point
- `package.json` — Dependencies including `@gram-ai/functions` and `zod`
- Build configuration

## Core API

### The Gram Class

```typescript
import Gram from "@gram-ai/functions";
import { z } from "zod/mini";

const gram = new Gram();
```

Constructor options:

| Option | Type | Description |
|---|---|---|
| `envSchema` | `Record<string, ZodSchema>` | Declare required environment variables |
| `lax` | `boolean` | Disable input validation (default: `false`) |
| `env` | `Record<string, string>` | Override `process.env` (testing) |
| `authInput` | `{ oauthVariable: string }` | Enable OAuth2 token passthrough |

### Registering Tools

```typescript
gram.tool({
  name: "get_weather",
  description: "Get current weather for a city",
  inputSchema: {
    city: z.string(),
    units: z.enum(["celsius", "fahrenheit"]).optional(),
  },
  async execute(ctx, input) {
    const data = await fetchWeather(input.city, input.units);
    return ctx.json(data);
  },
});
```

**Important**: `inputSchema` is a plain object of Zod schemas, not wrapped in `z.object()`.

### Tool Annotations

Optional hints about tool behavior:

```typescript
gram.tool({
  name: "delete_record",
  description: "Delete a record by ID",
  inputSchema: { id: z.string() },
  annotations: {
    title: "Delete Record",
    destructiveHint: true,
    readOnlyHint: false,
    idempotentHint: true,
    openWorldHint: true,
  },
  async execute(ctx, input) {
    await deleteRecord(input.id);
    return ctx.json({ deleted: true });
  },
});
```

### Exporting

**Every Gram Functions entry point must default-export the Gram instance:**

```typescript
export default gram;
```

## Context Methods (`ctx`)

The `execute` function receives a `ToolContext` as its first argument:

| Method | Content-Type | Returns |
|---|---|---|
| `ctx.json(data)` | `application/json` | `JSONResponse<T>` |
| `ctx.text(data)` | `text/plain;charset=UTF-8` | `TextResponse<T>` |
| `ctx.markdown(data)` | `text/markdown;charset=UTF-8` | `TextResponse<T>` |
| `ctx.html(data)` | `text/html` | `TextResponse<string>` |
| `ctx.fail(data, opts?)` | `application/json` | `never` (throws) |

### `ctx.fail()` — Error Responses

```typescript
async execute(ctx, input) {
  const user = await findUser(input.id);
  if (!user) {
    ctx.fail({ error: "User not found" }, { status: 404 });
    // Unreachable — ctx.fail() throws
  }
  return ctx.json(user);
}
```

### `ctx.env` — Environment Variables

Typed access to environment variables declared in `envSchema`:

```typescript
const gram = new Gram({
  envSchema: {
    API_TOKEN: z.string(),
    BASE_URL: z.string().url(),
  },
});

gram.tool({
  name: "call_api",
  inputSchema: { endpoint: z.string() },
  async execute(ctx, input) {
    const res = await fetch(`${ctx.env.BASE_URL}/${input.endpoint}`, {
      headers: { Authorization: `Bearer ${ctx.env.API_TOKEN}` },
    });
    return ctx.json(await res.json());
  },
});
```

### `ctx.signal` — Abort Signal

Propagate cancellation to async operations:

```typescript
async execute(ctx, input) {
  const res = await fetch(url, { signal: ctx.signal });
  return ctx.json(await res.json());
}
```

## `assert()` Utility

Validation helper that throws error Responses:

```typescript
import Gram, { assert } from "@gram-ai/functions";

gram.tool({
  name: "get_user",
  inputSchema: { id: z.string() },
  async execute(ctx, input) {
    assert(input.id.length > 0, { error: "ID must not be empty" }, { status: 400 });

    const user = await findUser(input.id);
    assert(user, { error: "User not found" }, { status: 404 });

    // TypeScript narrows: `user` is non-null here
    return ctx.json(user);
  },
});
```

`assert(condition, errorData, options?)`:
- If `condition` is falsy, throws a `Response` with JSON body
- Default status: `500`
- Provides TypeScript assertion narrowing

## Environment Variables (`envSchema`)

Declare environment variables your functions need at deploy time:

```typescript
const gram = new Gram({
  envSchema: {
    GITHUB_TOKEN: z.string().describe("GitHub personal access token"),
    SLACK_WEBHOOK: z.string().url().describe("Slack incoming webhook URL"),
    DEBUG: z.string().optional(),
  },
});
```

- Required vars cause a runtime error if missing
- `.describe()` adds documentation visible in the manifest
- `.optional()` makes a variable non-required
- `.default()` provides a fallback value

## Composing with `extend()`

Merge tools from multiple Gram instances:

```typescript
import Gram from "@gram-ai/functions";
import { z } from "zod/mini";

// API tools
const apiTools = new Gram({
  envSchema: { API_KEY: z.string() },
}).tool({
  name: "list_items",
  inputSchema: {},
  async execute(ctx) {
    const res = await fetch("https://api.example.com/items", {
      headers: { Authorization: `Bearer ${ctx.env.API_KEY}` },
    });
    return ctx.json(await res.json());
  },
});

// Utility tools
const utilTools = new Gram().tool({
  name: "format_date",
  inputSchema: { date: z.string() },
  async execute(ctx, input) {
    return ctx.text(new Date(input.date).toLocaleDateString());
  },
});

// Combine
const gram = apiTools.extend(utilTools);
export default gram;
```

`extend()` mutates the original instance and returns it for chaining.

## Example: Complete Function with External API

```typescript
import Gram, { assert } from "@gram-ai/functions";
import { z } from "zod/mini";

const gram = new Gram({
  envSchema: {
    WEATHER_API_KEY: z.string().describe("OpenWeather API key"),
  },
});

gram.tool({
  name: "get_weather",
  description: "Get current weather for a location",
  inputSchema: {
    city: z.string().describe("City name"),
    country: z.string().optional().describe("ISO country code"),
  },
  annotations: {
    readOnlyHint: true,
    openWorldHint: true,
  },
  async execute(ctx, input) {
    const query = input.country ? `${input.city},${input.country}` : input.city;
    const url = `https://api.openweathermap.org/data/2.5/weather?q=${encodeURIComponent(query)}&appid=${ctx.env.WEATHER_API_KEY}&units=metric`;

    const res = await fetch(url, { signal: ctx.signal });
    assert(res.ok, { error: `Weather API error: ${res.status}` }, { status: 502 });

    const data = await res.json();
    return ctx.json({
      city: data.name,
      temp: data.main.temp,
      description: data.weather[0].description,
    });
  },
});

export default gram;
```

## Building and Deploying

```bash
# Build (creates dist.zip with manifest.json)
gf build

# Deploy via gf CLI
gf push --project my-project

# Or deploy via gram CLI
gram stage function --slug my-tools --location ./dist.zip
gram push --config gram.deploy.json
```

## Build Configuration

Optional `gram.config.ts` in project root:

```typescript
import { defineConfig } from "@gram-ai/functions/build";

export default defineConfig({
  entrypoint: "src/gram.ts",  // default
  outDir: "dist",             // default
  slug: "my-tools",           // inferred from package.json if omitted
});
```

## What NOT to Do

- Do not wrap `inputSchema` in `z.object()` — pass a plain object of Zod schemas
- Do not forget `export default gram` — the runtime requires the default export
- Do not use `require()` — the SDK is ESM-only
- Do not deploy without building first — `gf build` generates the manifest
- Do not hardcode secrets in source — use `envSchema` and configure them in Gram

## Troubleshooting

| Problem | Solution |
|---|---|
| "manifest.json not found" on deploy | Run `gf build` before deploying |
| Type error on `ctx.env.X` | Ensure `X` is declared in `envSchema` |
| Tool not appearing after deploy | Check the tool is registered with `.tool()` before `export default` |
| `assert` not narrowing types | Import `assert` from `@gram-ai/functions`, not a test library |
| Input validation failing | Check Zod schema matches expected input shape |

## Related Skills

- **gram-context** — CLI reference and authentication
- **deploy-functions** — Deploy the built zip to Gram
- **deploy-openapi** — Deploy an OpenAPI spec alongside functions
