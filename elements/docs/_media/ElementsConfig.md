[**@gram-ai/elements v1.16.3**](../README.md)

***

[@gram-ai/elements](../globals.md) / ElementsConfig

# Interface: ElementsConfig

The top level configuration object for the Elements library.

## Example

```ts
const config: ElementsConfig = {
  mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
  projectSlug: 'my-project',
  systemPrompt: 'You are a helpful assistant.',
}
```

## Properties

### systemPrompt?

> `optional` **systemPrompt**: `string`

The system prompt to use for the Elements library.

***

### plugins?

> `optional` **plugins**: [`Plugin`](Plugin.md)[]

Any plugins to use for the Elements library.

#### Default

```ts
import { recommended } from '@gram-ai/elements/plugins'
```

***

### components?

> `optional` **components**: [`ComponentOverrides`](ComponentOverrides.md)

Override the default components used by the Elements library.

The available components are:
- Composer
- UserMessage
- EditComposer
- AssistantMessage
- ThreadWelcome
- Text
- Image
- ToolFallback
- Reasoning
- ReasoningGroup
- ToolGroup

To understand how to override these components, please consult the [assistant-ui documentation](https://www.assistant-ui.com/docs).

#### Example

```ts
const config: ElementsConfig = {
  components: {
    Composer: CustomComposerComponent,
  },
}
```

***

### projectSlug

> **projectSlug**: `string`

The project slug to use for the Elements library.

Your project slug can be found within the Gram dashboard.

#### Example

```ts
const config: ElementsConfig = {
  projectSlug: 'your-project-slug',
}
```

***

### mcp

> **mcp**: `string`

The Gram Server URL to use for the Elements library.
Can be retrieved from https://app.getgram.ai/{team}/{project}/mcp/{mcp_slug}

Note: This config option will likely change in the future

#### Example

```ts
const config: ElementsConfig = {
  mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
}
```

***

### chatEndpoint?

> `optional` **chatEndpoint**: `string`

The path of your backend's chat endpoint.

#### Default

```ts
'/chat/completions'
```

#### Example

```ts
const config: ElementsConfig = {
  chatEndpoint: '/my-custom-chat-endpoint',
}
```

***

### environment?

> `optional` **environment**: `Record`\<`string`, `unknown`\>

Custom environment variable overrides for the Elements library.
Will be used to override the environment variables for the MCP server.

For more documentation on passing through different kinds of environment variables, including bearer tokens, see the [Gram documentation](https://www.speakeasy.com/docs/gram/host-mcp/public-private-servers#pass-through-authentication).

***

### variant?

> `optional` **variant**: `"widget"` \| `"sidecar"` \| `"standalone"`

The layout variant for the chat interface.

- `widget`: A popup modal anchored to the bottom-right corner (default)
- `sidecar`: A side panel that slides in from the right edge of the screen
- `standalone`: A full-page chat experience

#### Default

```ts
'widget'
```

***

### model?

> `optional` **model**: [`ModelConfig`](ModelConfig.md)

LLM model configuration.

#### Example

```ts
const config: ElementsConfig = {
  model: {
    defaultModel: 'openai/gpt-4o',
    showModelPicker: true,
  },
}
```

***

### theme?

> `optional` **theme**: [`ThemeConfig`](ThemeConfig.md)

Visual appearance configuration options.
Similar to OpenAI ChatKit's ThemeOption.\

#### Example

```ts
const config: ElementsConfig = {
  theme: {
    colorScheme: 'dark',
    density: 'compact',
    radius: 'round',
  },
}
```

***

### welcome?

> `optional` **welcome**: [`WelcomeConfig`](WelcomeConfig.md)

The configuration for the welcome message and initial suggestions.

#### Example

```ts
const config: ElementsConfig = {
  welcome: {
    title: 'Welcome to the chat',
    subtitle: 'This is a chat with a bot',
    suggestions: [
      { title: 'Suggestion 1', label: 'Suggestion 1', action: 'action1' },
    ],
  },
}
```

***

### composer?

> `optional` **composer**: [`ComposerConfig`](ComposerConfig.md)

The configuration for the composer.

#### Example

```ts
const config: ElementsConfig = {
  composer: {
    placeholder: 'Enter your message...',
  },
}
```

***

### modal?

> `optional` **modal**: [`ModalConfig`](ModalConfig.md)

The configuration for the modal window.
Does not apply if variant is 'standalone'.

#### Example

```ts
const config: ElementsConfig = {
  modal: {
    title: 'Chat',
    position: 'bottom-right',
    expandable: true,
    defaultExpanded: false,
    dimensions: {
      default: {
        width: 400,
        height: 600,
      },
    },
  },
}
```

***

### sidecar?

> `optional` **sidecar**: [`SidecarConfig`](SidecarConfig.md)

The configuration for the sidecar panel.
Only applies if variant is 'sidecar'.

#### Example

```ts
const config: ElementsConfig = {
  sidecar: {
    title: 'Chat',
    expandable: true,
    defaultExpanded: false,
    dimensions: {
      default: {
        width: 400,
        height: 600,
      },
    },
  },
}
```

***

### tools?

> `optional` **tools**: [`ToolsConfig`](ToolsConfig.md)

The configuration for the tools.

#### Example

```ts
const config: ElementsConfig = {
  tools: {
    expandToolGroupsByDefault: true,
    frontendTools: {
      fetchUrl: FetchTool,
    },
    components: {
      fetchUrl: FetchToolComponent,
    },
  },
}
```
