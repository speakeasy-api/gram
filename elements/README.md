# `@gram-ai/elements`

Elements is a library built for the agentic age. We provide customizable and elegant UI primitives for building chat-like experiences for MCP Servers. Works best with Gram MCP, but also supports connecting any MCP Server.

## Setup

### Frontend Setup

The easiest way to install Elements and all its peer dependencies:

```bash
npx @gram-ai/elements install
```

This will automatically detect your package manager and install everything you need.

<details>
<summary>Manual installation</summary>

If you prefer to install manually, first install the peer dependencies:

```bash
pnpm add react react-dom motion remark-gfm zustand vega shiki
```

Then install Elements:

```bash
pnpm add @gram-ai/elements
```

</details>

### Backend Setup

If you're only using the server handlers (`@gram-ai/elements/server`), you can install without needing to install any of the React peer dependencies:

```bash
pnpm add @gram-ai/elements
```

> **Note:** Your package manager may show peer dependency warnings for React packages. These warnings are safe to ignore when using only `/server` exports, as React dependencies are marked as optional.

## Setting up your backend

We provide framework-specific adapters to make it easy to set up a backend endpoint for Gram Elements. These adapters handle the session token creation securely on your server without exposing your `GRAM_API_KEY` to the browser.

### Express

```typescript
import { createExpressHandler } from '@gram-ai/elements/server/express'
import express from 'express'

const app = express()
app.use(express.json())

app.post('/chat/session', createExpressHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600, // optional, max 3600 seconds
}))

app.listen(3000)
```

For dynamic options based on the request:

```typescript
app.post('/chat/session', createExpressHandler((req) => ({
  embedOrigin: req.headers.origin || 'http://localhost:3000',
  userIdentifier: req.user?.id || 'anonymous',
  expiresAfter: 3600,
})))
```

### Next.js App Router

```typescript
// app/api/chat/session/route.ts
import { createNextHandler } from '@gram-ai/elements/server/nextjs'

export const POST = createNextHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})
```

For dynamic options:

```typescript
// app/api/chat/session/route.ts
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

fastify.post('/chat/session', createFastifyHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
}))

fastify.listen({ port: 3000 })
```

### Hono

```typescript
import { createHonoHandler } from '@gram-ai/elements/server/hono'
import { Hono } from 'hono'

const app = new Hono()

app.post('/chat/session', createHonoHandler({
  embedOrigin: 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
}))

export default app
```

### Environment Variables

All adapters require the `GRAM_API_KEY` environment variable to be set:

```bash
GRAM_API_KEY=xxx
```

This key is kept on your server and never exposed to the browser, ensuring secure communication with Gram's API.

## Setting up your frontend

`@gram-ai/elements` requires that you wrap your React tree with our context provider:

```jsx
import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'

// Please fill out projectSlug and mcp
const config: ElementsConfig = {
  projectSlug: 'xxx',
  mcp: 'https://app.getgram.ai/mcp/{your_slug}',
  variant: 'widget',
  welcome: {
    title: 'Hello!',
    subtitle: 'How can I help you today?',
  },
  composer: {
    placeholder: 'Ask me anything...',
  },
  modal: {
    defaultOpen: true,
  },
}

export const App = () => {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  )
}
```

### Custom Session Endpoint

By default, Elements expects the session endpoint on your backend to be located at `/chat/session`. If you've mounted your session endpoint at a different path, you can provide a custom `getSession` function in the `ElementsProvider`:

```jsx
import { GramElementsProvider, Chat, type ElementsConfig, type GetSessionFn } from '@gram-ai/elements'

const config: ElementsConfig = {
  projectSlug: 'xxx',
  mcp: 'https://app.getgram.ai/mcp/{your_slug}',
  // ... other config
}

const customGetSession: GetSessionFn = async () => {
  const response = await fetch('/api/custom-session-endpoint', {
    method: 'POST',
  })
  const data = await response.json()
  return data.client_token
}

export const App = () => {
  return (
    <GramElementsProvider config={config} getSession={customGetSession}>
      <Chat />
    </GramElementsProvider>
  )
}
```

## Configuration

For complete configuration options and TypeScript type definitions, see the [API documentation](./docs/interfaces/ElementsConfig.md).

### Quick Configuration Example

```typescript
import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'

const config: ElementsConfig = {
  projectSlug: 'your-project',
  mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
  variant: 'widget', // 'widget' | 'sidecar' | 'standalone'
  welcome: {
    title: 'Hello!',
    subtitle: 'How can I help you today?',
  },
}

export const App = () => {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  )
}
```

## Plugins

Plugins extend the Gram Elements library with custom rendering capabilities for specific content types. They allow you to transform markdown code blocks into rich, interactive visualizations and components.

### How Plugins Work

When you add a plugin:

1. The plugin extends the system prompt with instructions for the LLM
2. The LLM returns code blocks marked with the plugin's language identifier
3. The plugin's custom component renders the code block content

For example, the built-in chart plugin instructs the LLM to return Vega specifications for visualizations, which are then rendered as interactive charts.

### Using Recommended Plugins

Gram Elements includes a set of recommended plugins that you can use out of the box:

```typescript
import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'
import { recommended } from '@gram-ai/elements/plugins'

const config: ElementsConfig = {
  projectSlug: 'my-project',
  mcp: 'https://app.getgram.ai/mcp/my-mcp-slug',
  welcome: {
    title: 'Hello!',
    subtitle: 'How can I help you today?',
  },
  // Add all recommended plugins
  plugins: recommended,
}

export const App = () => {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  )
}
```

#### Available Recommended Plugins

- **`chart`** - Renders Vega chart specifications as interactive visualizations

### Using Individual Plugins

You can also import and use plugins individually:

```typescript
import { chart } from '@gram-ai/elements/plugins'

const config: ElementsConfig = {
  // ... other config
  plugins: [chart],
}
```

### Using Custom Plugins

You can create your own custom plugins to add specialized rendering capabilities:

```typescript
import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'
import { chart } from '@gram-ai/elements/plugins'
import { myCustomPlugin } from './plugins/myCustomPlugin'

const config: ElementsConfig = {
  projectSlug: 'my-project',
  mcp: 'https://app.getgram.ai/mcp/my-mcp-slug',
  welcome: {
    title: 'Hello!',
    subtitle: 'How can I help you today?',
  },
  // Combine built-in and custom plugins
  plugins: [chart, myCustomPlugin],
}

export const App = () => {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  )
}
```

### Creating Custom Plugins

To create your own plugins, see the comprehensive [Plugin Development Guide](./src/plugins/README.md). The guide covers:

- Plugin architecture and interface
- Step-by-step tutorial for creating plugins
- Best practices and common patterns
- Real-world examples
- Troubleshooting tips

## Replay

Replay lets you play back a pre-recorded conversation with streaming animations — no auth, MCP, or network calls required. This is useful for marketing demos, documentation, and testing.

### Recording a cassette

A cassette is a JSON file that captures a conversation. To record one from a live chat session, enable the built-in recorder by setting this environment variable:

```
VITE_ELEMENTS_ENABLE_CASSETTE_RECORDING=true
```

This adds a record button to the composer. Click it to open the recorder popover, then start recording. Chat normally, and when you're done, click **Stop & Download** to save the cassette as a `.cassette.json` file.

You can also record programmatically using the `useRecordCassette` hook:

```tsx
import {
  GramElementsProvider,
  Chat,
  useRecordCassette,
} from '@gram-ai/elements'

function RecordableChat() {
  const { isRecording, startRecording, download } = useRecordCassette()
  return (
    <>
      <Chat />
      <button onClick={startRecording}>Record</button>
      <button onClick={() => download('demo')}>Save</button>
    </>
  )
}
```

### Playing back a cassette

Use the `<Replay>` component, which replaces `GramElementsProvider` entirely:

```tsx
import { Replay, Chat } from '@gram-ai/elements'
import cassette from './demo.cassette.json'

function MarketingDemo() {
  return (
    <Replay cassette={cassette} config={{ variant: 'standalone' }}>
      <Chat />
    </Replay>
  )
}
```

`<Replay>` accepts optional timing props to control the playback speed:

| Prop                  | Default | Description                                     |
| --------------------- | ------- | ----------------------------------------------- |
| `typingSpeed`         | `15`    | Milliseconds per character for streaming        |
| `userMessageDelay`    | `800`   | Milliseconds before each user message appears   |
| `assistantStartDelay` | `400`   | Milliseconds before the assistant starts typing |
| `onComplete`          | —       | Callback when replay finishes                   |

## React Compatibility

`@gram-ai/elements` supports React 16.8+, React 17, React 18, and React 19.

React 18 and 19 work out of the box. For React 16 or 17, you need to configure your bundler to shim newer React APIs (`useSyncExternalStore`, `useId`, `useInsertionEffect`) that are used by transitive dependencies like zustand and @assistant-ui/react.

> **Note:** React 16 and React 17 are not regularly tested. If you run into any issues using Elements with these versions, please reach out to us for support.

### Vite Setup (React 16/17)

Add the compatibility plugin to your Vite config:

```typescript
import react from '@vitejs/plugin-react'
import { reactCompat } from '@gram-ai/elements/compat'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), reactCompat()],
})
```

This automatically shims React 18 APIs (`useSyncExternalStore`, `useId`, `useInsertionEffect`) so that Elements and its dependencies work correctly on older React versions.

## Contributing

We welcome pull requests to Elements. Please read the contributing guide.
