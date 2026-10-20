# Hello from Speakeasy Functions!

This project builds and deploys [Speakeasy Functions](https://www.speakeasy.com/docs/ai-control-plane/mcp-gateway/building-servers/functions) using a
tiny TypeScript framework that looks like this:

```ts
import { Functions } from "@speakeasy-api/functions";
import * as z from "zod/mini";

const functions = new Functions().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export default functions;
```

Speakeasy Functions are tools for LLMs and MCP servers that can do arbitrary tasks
such as fetching data from APIs, performing calculations, or interacting with
hosted databases.

## Quick start

To get started, install dependencies and run the development server:

```bash
pnpm install
```

To build a zip file that can be deployed to Speakeasy, run:

```bash
pnpm build
```

After building, push your function to Speakeasy with:

```bash
pnpm push
```

## Testing Locally

If you want to poke at the tools you've built during local development, you can
start a local MCP server over stdio transport with:

```bash
pnpm dev
```

Specifically, this command will spin up [MCP inspector][mcp-inspector] to let
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector

## What next?

To learn more about using the framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md)
