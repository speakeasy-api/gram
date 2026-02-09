**@gram-ai/elements v1.25.2**

***

# @gram-ai/elements

## First time setup

Please follow the [Setup Instructions](_media/README.md#setup) in the main README to get started.

## Elements Configuration

The minimal configuration required to get Elements setup is demonstrated below:

```ts
import type { ElementsConfig } from '@gram-ai/elements'

const config: ElementsConfig = {
  mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
  projectSlug: 'your-project-slug',
}
```

The `mcp` and `projectSlug` values can be retrieved from the MCP and project pages respectively.

## React Compatibility

`@gram-ai/elements` supports React 16.8+, React 17, React 18, and React 19. React 18 and 19 work out of the box. For React 16 or 17, add the compatibility plugin to your Vite config:

```ts
import { reactCompat } from '@gram-ai/elements/compat'

export default defineConfig({
  plugins: [react(), reactCompat()],
})
```

React 16 and React 17 are not regularly tested — please reach out to us for support if you run into any issues with these versions.

## API Documentation

`ElementsConfig` is the top level configuration object for the Elements library. Please refer the [ElementsConfig](_media/ElementsConfig.md) interface documentation for more details on how to configure Elements.

For an overview of all the available types and functions, please refer to the [Globals](_media/globals.md) documentation.
