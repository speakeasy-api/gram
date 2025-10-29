# TypeScript MCP server Framework

This library provides a lightweight framework for building MCP compliant agentic tools in
TypeScript using Gram Functions.

Gram Functions are small pieces of code that represent LLM tools. They are
deployed to [Gram](https://getgram.ai) where they can be combined with other tools to create MCP
servers.

"Hello, World!" example:

```typescript
import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const g = new Gram().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export const handleToolCall = g.handleToolCall;
```

## Quickstart

You can use one of the following command to scaffold a new Gram Function project quickly:

```
pnpm create @gram-ai/function@latest --template gram

## Or one of the following:
# bun create @gram-ai/function@latest --template gram
# npm create @gram-ai/function@latest -- --template gram
```

## Installation

Use one of the following commands to add the package to your project:

```
pnpm add @gram-ai/functions

## Or one of the following:
# bun add @gram-ai/functions
# npm add @gram-ai/functions
```

## Core Concepts

### The Gram Instance

The `Gram` class is the main entry point for defining tools. You create an instance and chain `.tool()` calls to register multiple tools:

```typescript
import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const g = new Gram()
  .tool({
    name: "add",
    description: "Add two numbers",
    inputSchema: { a: z.number(), b: z.number() },
    async execute(ctx, input) {
      return ctx.json({ sum: input.a + input.b });
    },
  })
  .tool({
    name: "multiply",
    description: "Multiply two numbers",
    inputSchema: { a: z.number(), b: z.number() },
    async execute(ctx, input) {
      return ctx.json({ product: input.a * input.b });
    },
  });

export const handleToolCall = g.handleToolCall;
```

### Tool Definition

Each tool requires:

- **name**: A unique identifier for the tool
- **description** (optional): Human-readable description of what the tool does
- **inputSchema**: A Zod schema object defining the expected input parameters
- **variables** (optional): Environment variables the tool needs
- **execute**: An async function that implements the tool logic

### Tool Context

The `execute` function receives a `ctx` (context) object with helper methods:

#### `ctx.json(data)`

Returns a JSON response:

```typescript
async execute(ctx, input) {
  return ctx.json({ result: "success", value: 42 });
}
```

#### `ctx.text(data)`

Returns a plain text response:

```typescript
async execute(ctx, input) {
  return ctx.text("Operation completed successfully");
}
```

#### `ctx.html(data)`

Returns an HTML response:

```typescript
async execute(ctx, input) {
  return ctx.html("<h1>Hello, World!</h1>");
}
```

#### `ctx.fail(data, options?)`

Throws an error response (never returns):

```typescript
async execute(ctx, input) {
  if (!input.value) {
    ctx.fail({ error: "value is required" }, { status: 400 });
  }
  // ...
}
```

#### `ctx.signal`

An `AbortSignal` for handling cancellation:

```typescript
async execute(ctx, input) {
  const response = await fetch(input.url, { signal: ctx.signal });
  return ctx.json(await response.json());
}
```

#### `ctx.vars`

Access to environment variables defined in the tool's `variables` property:

```typescript
.tool({
  name: "api_call",
  inputSchema: { endpoint: z.string() },
  variables: {
    API_KEY: { description: "API key for authentication" }
  },
  async execute(ctx, input) {
    const apiKey = ctx.vars.API_KEY;
    // Use apiKey...
  },
})
```

## Input Validation

Input schemas are defined using [Zod](https://zod.dev/):

```typescript
import * as z from "zod/mini";

.tool({
  name: "create_user",
  inputSchema: {
    email: z.string().check(z.email()),
    age: z.number().check(z.min(18)),
    name: z.optional(z.string()),
  },
  async execute(ctx, input) {
    // input is fully typed based on the schema
    return ctx.json({ userId: "123" });
  },
})
```

### Lax Mode

By default, the framework strictly validates input. You can enable lax mode to allow unvalidated input to pass through:

```typescript
const g = new Gram({ lax: true });
```

## Environment Variables

### Runtime Environment

Pass environment variables are read from `process.env` by default, but you can override them when creating the `Gram` instance:

```typescript
const g = new Gram({
  env: {
    API_KEY: "secret-key",
    BASE_URL: "https://api.example.com",
  },
});
```

If not provided, the framework falls back to `process.env`.

### Tool Variables

Declare which environment variables a tool needs:

```typescript
.tool({
  name: "weather",
  inputSchema: { city: z.string() },
  variables: {
    WEATHER_API_KEY: {
      description: "API key for weather service"
    }
  },
  async execute(ctx, input) {
    const apiKey = ctx.vars.WEATHER_API_KEY;
    // Make API call...
  },
})
```

## Response Types

The framework supports multiple response types. All response methods return Web API `Response` objects.

### JSON Response

```typescript
return ctx.json({
  status: "success",
  data: { id: 123, name: "Example" },
});
```

### Text Response

```typescript
return ctx.text("Plain text response");
```

### HTML Response

```typescript
return ctx.html(`
  <!DOCTYPE html>
  <html>
    <body><h1>Hello</h1></body>
  </html>
`);
```

### Custom Response

You can also return a plain `Response` object:

```typescript
return new Response(data, {
  status: 200,
  headers: {
    "Content-Type": "application/xml",
    "X-Custom-Header": "value",
  },
});
```

## Error Handling

### Using `ctx.fail()`

Use `ctx.fail()` to throw error responses:

```typescript
async execute(ctx, input) {
  if (!input.userId) {
    ctx.fail(
      { error: "userId is required" },
      { status: 400 }
    );
  }

  const user = await fetchUser(input.userId);
  if (!user) {
    ctx.fail(
      { error: "User not found" },
      { status: 404 }
    );
  }

  return ctx.json({ user });
}
```

Errors automatically include a stack trace in the response.

### Using `assert()`

The `assert` function provides a convenient way to validate conditions and throw error responses:

```typescript
import { assert } from "@gram-ai/functions";

async execute(ctx, input) {
  assert(input.userId, { error: "userId is required" }, { status: 400 });

  const user = await fetchUser(input.userId);
  assert(user, { error: "User not found" }, { status: 404 });

  return ctx.json({ user });
}
```

The `assert` function throws a `Response` object when the condition is false. The framework catches all thrown values, and if any happen to be a `Response` instance, they will be returned to the client.

Key points about `assert`:
- First parameter is the condition to check
- Second parameter is the error data (must include an `error` field)
- Third parameter is optional and can specify the status code (defaults to 500)
- Automatically includes a stack trace in the response
- Uses TypeScript's assertion type to narrow types when the assertion passes

## Manifest Generation

Generate a manifest of all registered tools:

```typescript
const g = new Gram()
  .tool({
    /* ... */
  })
  .tool({
    /* ... */
  });

const manifest = g.manifest();
// {
//   version: "0.0.0",
//   tools: [
//     {
//       name: "tool1",
//       description: "...",
//       inputSchema: "...", // JSON Schema string
//       variables: { ... }
//     },
//     ...
//   ]
// }
```

## Handling Tool Calls

Export the `handleToolCall` method to process incoming requests:

```typescript
const g = new Gram()
  .tool({
    /* ... */
  })
  .tool({
    /* ... */
  });

export const handleToolCall = g.handleToolCall;
```

You can also call tools programmatically:

```typescript
const response = await g.handleToolCall({
  name: "add",
  input: { a: 5, b: 3 },
});

const data = await response.json();
console.log(data); // { sum: 8 }
```

With abort signal support:

```typescript
const controller = new AbortController();

const responsePromise = g.handleToolCall(
  { name: "longRunning", input: {} },
  { signal: controller.signal },
);

// Cancel after 5 seconds
setTimeout(() => controller.abort(), 5000);
```

## Type Safety

The framework provides full TypeScript type inference:

```typescript
const g = new Gram().tool({
  name: "greet",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    // input.name is typed as string
    return ctx.json({ message: `Hello, ${input.name}` });
  },
});

// Type-safe tool calls
const response = await g.handleToolCall({
  name: "greet", // Only "greet" is valid
  input: { name: "World" }, // input is typed correctly
});

// Response type is inferred
const data = await response.json(); // { message: string }
```
