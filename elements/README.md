# `@gram-ai/elements`

Elements is a library built for the agentic age. We provide customizable and elegant UI primitives for building chat-like experiences for MCP Servers. Works best with Gram MCP, but also supports connecting any MCP Server.

## Setup

### Frontend Setup

First ensure that you have installed the required peer dependencies:

```bash
pnpm add react react-dom motion remark-gfm zustand vega shiki
```

Then install Elements:

```bash
pnpm add @gram-ai/elements
```

### Backend Setup

If you're only using the server handlers (`@gram-ai/elements/server`), you can install without React:

```bash
pnpm add @gram-ai/elements
```

> **Note:** Your package manager may show peer dependency warnings for React packages. These warnings are safe to ignore when using only `/server` exports, as React dependencies are marked as optional.

## Setting up your backend

At the moment, we provide a set of handlers via the `@gram-ai/elements/server` package that you can use to automatically setup your backend for usage with Gram Elements. The example below demonstrates the setup for Express:

```typescript
import { createElementsServerHandlers } from '@gram-ai/elements/server'
import express from 'express'

const handlers = createElementsServerHandlers()
const app = express()

app.use(express.json())

app.post('/chat/session', (req, res) =>
  handlers.session(req, res, {
    // The origin from which the token will be used
    embedOrigin: 'http://localhost:3000',
    // A free-form user identifier
    userIdentifier: 'user-123',
    // Token expiration in seconds (max / default 3600)
    expiresAfter: 3600,
  })
)
```

You will need to add an environment variable to your backend and make it available to the process:

```
GRAM_API_KEY=xxx
```

This will enable your backend chat endpoint to talk to our servers securely.

## Setting up your frontend

`@gram-ai/elements` requires that you wrap your React tree with our context provider and reference our CSS:

```jsx
import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'

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
import '@gram-ai/elements/elements.css'

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
import '@gram-ai/elements/elements.css'

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
import '@gram-ai/elements/elements.css'

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
import '@gram-ai/elements/elements.css'

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

## Contributing

We welcome pull requests to Elements. Please read the contributing guide.
