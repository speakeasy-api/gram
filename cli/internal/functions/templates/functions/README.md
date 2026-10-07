# Hello from Speakeasy Functions!

This project builds and deploys [Speakeasy Functions](https://www.speakeasy.com) using a
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

## Prerequisites

- [Node.js](https://nodejs.org) version 22.18.0 or later
- The Speakeasy AI Control Plane CLI: `brew install speakeasy-api/tap/cli` or
  `npm i -g @speakeasy-api/cli`

## Quick start

To get started, install dependencies with your package manager:

```bash
npm install
```

To build a zip file that can be deployed to the Speakeasy AI Control Plane, run:

```bash
speakeasy functions build
```

Then deploy your function with:

```bash
speakeasy functions push
```

The `build` and `push` package scripts run the same commands.

## Testing Locally

If you want to poke at the tools you've built during local development, you can
start a local MCP server over stdio transport with:

```bash
speakeasy functions dev
```

Specifically, this command will spin up [MCP inspector][mcp-inspector] to let
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector

## What next?

To learn more about using the framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md)
