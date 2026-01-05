# @gram-ai/elements

## First time setup

Please follow the [Setup Instructions](./README.md#setup) in the main README to get started.

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

## API Documentation

`ElementsConfig` is the top level configuration object for the Elements library. Please refer the [ElementsConfig](./docs/interfaces/ElementsConfig.md) interface documentation for more details on how to configure Elements.

For an overview of all the available types and functions, please refer to the [Globals](./docs/globals.md) documentation.
