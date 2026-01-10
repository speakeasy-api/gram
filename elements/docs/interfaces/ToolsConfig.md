[**@gram-ai/elements v1.18.3**](../README.md)

***

[@gram-ai/elements](../globals.md) / ToolsConfig

# Interface: ToolsConfig

ToolsConfig is used to configure tool support in the Elements library.
At the moment, you can override the default React components used by
individual tool results.

## Example

```ts
const config: ElementsConfig = {
  tools: {
    components: {
      "get_current_weather": WeatherComponent,
    },
  },
}
```

## Properties

### expandToolGroupsByDefault?

> `optional` **expandToolGroupsByDefault**: `boolean`

Whether individual tool calls within a group should be expanded by default.

#### Default

```ts
false
```

***

### components?

> `optional` **components**: `Record`\<`string`, `ToolCallMessagePartComponent` \| `undefined`\>

`components` can be used to override the default components used by the
Elements library for a given tool result.

Please ensure that the tool name directly matches the tool name in your Gram toolset.

#### Example

```ts
const config: ElementsConfig = {
  tools: {
    components: {
      "get_current_weather": WeatherComponent,
    },
  },
}
```

***

### frontendTools?

> `optional` **frontendTools**: `Record`\<`string`, `AssistantTool`\>

The frontend tools to use for the Elements library.

#### Examples

```ts
import { defineFrontendTool } from '@gram-ai/elements'

const FetchTool = defineFrontendTool<{ url: string }, string>(
  {
    description: 'Fetch a URL (supports CORS-enabled URLs like httpbin.org)',
    parameters: z.object({
      url: z.string().describe('URL to fetch (must support CORS)'),
    }),
    execute: async ({ url }) => {
      const response = await fetch(url as string)
      const text = await response.text()
      return text
    },
  },
  'fetchUrl'
)
const config: ElementsConfig = {
  tools: {
    frontendTools: {
      fetchUrl: FetchTool,
    },
  },
}
```

You can also override the default components used by the
Elements library for a given tool result.

```ts
import { FetchToolComponent } from './components/FetchToolComponent'

const config: ElementsConfig = {
  tools: {
    frontendTools: {
      fetchUrl: FetchTool,
    },
    components: {
      'fetchUrl': FetchToolComponent, // will override the default component used by the Elements library for the 'fetchUrl' tool
    },
  },
}
```

***

### toolsRequiringApproval?

> `optional` **toolsRequiringApproval**: `string`[]

List of tool names that require confirmation from the end user before
being executed. The user can choose to approve once or approve for the
entire session via the UI.

#### Example

```ts
tools: {
  toolsRequiringApproval: ['delete_file', 'send_email'],
}
```
