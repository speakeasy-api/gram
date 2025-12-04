# OpenAI Apps SDK Example

This is an example project demonstrating how to use the OpenAI Apps SDK.

## Building the MCP Server

The first step is to build the Pizza app from the `/src` directory and inline
its build assets into an MCP server. Do this by running the following commands
from the project root:

```bash
pnpm install
pnpm build
```

Next, `cd` into the `pizzaz_node_server/mcp-server` directory and run:

```bash
pnpm i @gram-ai/functions
pnpm run inline:app
```

## Deploying to Gram

Once the MCP server is built, it can be deployed to Gram via Gram functions.

```bash
pnpm build
gram auth
pnpm push
```

For more details about this example, refer to the gram
[documentation](https://www.speakeasy.com/docs/gram/examples/open-ai-apps-sdk).
