# Gram Elements Server Adapters

Server adapters allow you to securely create chat session tokens without exposing your `GRAM_API_KEY` to the browser. Each adapter is framework-specific and provides a type-safe API for handling session creation.

## Available Adapters

- **Express** - `@gram-ai/elements/server/express`
- **Next.js App Router** - `@gram-ai/elements/server/nextjs`
- **Fastify** - `@gram-ai/elements/server/fastify`
- **Hono** - `@gram-ai/elements/server/hono`

## Installation

Install the base package:

```bash
pnpm add @gram-ai/elements
```

Then install your framework of choice (if not already installed):

```bash
# Express
pnpm add express @types/express

# Next.js (comes with Next.js)
pnpm add next

# Fastify
pnpm add fastify

# Hono
pnpm add hono
```

## Environment Setup

All adapters require the `GRAM_API_KEY` environment variable:

```bash
GRAM_API_KEY=your-api-key-here
```

Optionally, you can set a custom Gram API URL:

```bash
GRAM_API_URL=https://app.getgram.ai  # default value
```

## Usage Examples

### Express

```typescript
import { createExpressHandler } from '@gram-ai/elements/server/express'
import express from 'express'

const app = express()
app.use(express.json())

// Static configuration
app.post('/chat/session', createExpressHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600, // optional, defaults to 3600 (1 hour)
}))

// Dynamic configuration based on request
app.post('/chat/session', createExpressHandler((req) => ({
  embedOrigin: req.headers.origin || 'http://localhost:3000',
  userIdentifier: req.user?.id || 'anonymous',
  expiresAfter: 3600,
})))

app.listen(3000)
```

### Next.js App Router

```typescript
// app/api/chat/session/route.ts
import { createNextHandler } from '@gram-ai/elements/server/nextjs'

// Static configuration
export const POST = createNextHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})
```

```typescript
// app/api/chat/session/route.ts - with dynamic configuration
import { createNextHandler } from '@gram-ai/elements/server/nextjs'
import { cookies } from 'next/headers'

export const POST = createNextHandler(async (request) => {
  const cookieStore = await cookies()
  const userId = cookieStore.get('userId')?.value || 'anonymous'

  return {
    embedOrigin: request.headers.get('origin') || 'http://localhost:3000',
    userIdentifier: userId,
    expiresAfter: 3600,
  }
})
```

### Fastify

```typescript
import { createFastifyHandler } from '@gram-ai/elements/server/fastify'
import Fastify from 'fastify'

const fastify = Fastify()

// Static configuration
fastify.post('/chat/session', createFastifyHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
}))

// Dynamic configuration
fastify.post('/chat/session', createFastifyHandler(async (request) => ({
  embedOrigin: request.headers.origin || 'http://localhost:3000',
  userIdentifier: request.user?.id || 'anonymous',
  expiresAfter: 3600,
})))

fastify.listen({ port: 3000 })
```

### Hono

```typescript
import { createHonoHandler } from '@gram-ai/elements/server/hono'
import { Hono } from 'hono'

const app = new Hono()

// Static configuration
app.post('/chat/session', createHonoHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
}))

// Dynamic configuration
app.post('/chat/session', createHonoHandler(async (c) => ({
  embedOrigin: c.req.header('origin') || 'http://localhost:3000',
  userIdentifier: c.get('user')?.id || 'anonymous',
  expiresAfter: 3600,
})))

export default app
```

## Configuration Options

All adapters accept a `SessionHandlerOptions` object or a function that returns one:

```typescript
interface SessionHandlerOptions {
  /**
   * The origin from which the token will be used.
   * This should match the origin where your frontend is hosted.
   */
  embedOrigin: string

  /**
   * Free-form user identifier.
   * Can be any string that uniquely identifies the user.
   */
  userIdentifier: string

  /**
   * Token expiration in seconds.
   * Maximum and default value is 3600 (1 hour).
   * @default 3600
   */
  expiresAfter?: number
}
```

## Request Headers

The client must send a `Gram-Project` header with the project slug:

```typescript
fetch('/chat/session', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Gram-Project': 'your-project-slug',
  },
})
```

## Response Format

On success, the adapter returns a JSON response with the session token:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expiresAt": "2024-01-01T12:00:00Z"
}
```

On error:

```json
{
  "error": "Error message"
}
```

## Frontend Integration

Use the session token in your frontend with `GramElementsProvider`:

```typescript
import { GramElementsProvider } from '@gram-ai/elements'

const config = {
  projectSlug: 'your-project-slug',
  mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
  sessionFn: async () => {
    const response = await fetch('/chat/session', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Gram-Project': 'your-project-slug',
      },
    })
    const data = await response.json()
    return data.token
  },
}
```

## Migrating from Legacy API

If you're using the deprecated `createElementsServerHandlers()` API:

```typescript
// Old (deprecated)
import { createElementsServerHandlers } from '@gram-ai/elements/server'

const handlers = createElementsServerHandlers()
app.post('/chat/session', (req, res) =>
  handlers.session(req, res, {
    embedOrigin: 'http://localhost:3000',
    userIdentifier: 'user-123',
  })
)
```

Migrate to the framework-specific adapter:

```typescript
// New (recommended)
import { createExpressHandler } from '@gram-ai/elements/server/express'

app.post('/chat/session', createExpressHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
}))
```

## Security Considerations

1. **Never expose your `GRAM_API_KEY`** - Keep it server-side only
2. **Validate user identity** - Use your authentication system to determine the `userIdentifier`
3. **Restrict `embedOrigin`** - Set it to your actual frontend origin, not a wildcard
4. **Use HTTPS in production** - Always use secure connections for production deployments
5. **Set appropriate token expiration** - Use the shortest expiration time that works for your use case

## Troubleshooting

### Missing `Gram-Project` header

Ensure your client sends the `Gram-Project` header with every request to the session endpoint.

### `GRAM_API_KEY` not found

Make sure the environment variable is set and accessible to your server process.

### CORS issues

Configure CORS on your server to allow requests from your frontend origin:

```typescript
// Express example
import cors from 'cors'
app.use(cors({
  origin: 'http://localhost:3000',
  credentials: true,
}))
```

## Framework-Specific Notes

### Express
- Requires `express.json()` middleware for parsing request bodies
- Works with Express 4.x and 5.x

### Next.js
- Only works with App Router (Next.js 13+)
- Automatically handles JSON parsing
- Use `async` functions for dynamic configuration

### Fastify
- Compatible with Fastify 4.x and 5.x
- Automatically handles JSON parsing
- Use `async` functions for dynamic configuration

### Hono
- Works on any runtime: Node.js, Cloudflare Workers, Deno, Bun
- Minimal API surface, highly performant
- Use `async` functions for dynamic configuration
