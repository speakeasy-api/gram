[**@gram-ai/elements v1.21.3**](../README.md)

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

### mcp?

> `optional` **mcp**: `string`

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

### environment?

> `optional` **environment**: `Record`\<`string`, `unknown`\>

Custom environment variable overrides for the Elements library.
Will be used to override the environment variables for the MCP server.

For more documentation on passing through different kinds of environment variables, including bearer tokens, see the [Gram documentation](https://www.speakeasy.com/docs/gram/host-mcp/public-private-servers#pass-through-authentication).

***

### gramEnvironment?

> `optional` **gramEnvironment**: `string`

The environment slug to use for resolving secrets.
When specified, this is sent as the Gram-Environment header to select
which environment's secrets to use for tool execution.

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

### languageModel?

> `optional` **languageModel**: `LanguageModel`

Optional property to override the LLM provider. If you override the model,
then logs & usage metrics will not be tracked directly via Gram.

Please ensure that you are using an AI SDK v2 compatible model (e.g a
Vercel AI sdk provider in the v2 semver range), as this is the only variant
compatible with AI SDK V5

Example with Google Gemini:
```ts
import { google } from '@ai-sdk/google';

const googleGemini = google('gemini-3-pro-preview');

const config: ElementsConfig = {
  {other options}
  languageModel: googleGemini,
}
```

***

### modal?

> `optional` **modal**: [`ModalConfig`](ModalConfig.md)

The configuration for the modal window.
Only applicable if variant is 'widget'.

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

***

### history?

> `optional` **history**: [`HistoryConfig`](HistoryConfig.md)

Configuration for chat history and thread persistence.
When enabled, conversations are saved and the thread list is shown.

#### Example

```ts
const config: ElementsConfig = {
  history: {
    enabled: true,
    showThreadList: true,
  },
}
```

***

### api?

> `optional` **api**: `ApiConfig`

The API configuration to use for the Elements library.

Use this to override the default API URL, or add explicit auth configuration

#### Example

```ts
const config: ElementsConfig = {
  api: {
    url: 'https://api.getgram.ai',
  },
}
```

***

### errorTracking?

> `optional` **errorTracking**: [`ErrorTrackingConfigOption`](ErrorTrackingConfigOption.md)

Error tracking configuration.
By default, errors are reported to help improve the Elements library.

#### Example

```ts
const config: ElementsConfig = {
  errorTracking: {
    enabled: false, // Opt out of error reporting
  },
}
```
