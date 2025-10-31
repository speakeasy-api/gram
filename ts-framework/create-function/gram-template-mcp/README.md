# Gram Function MCP Template

This template allows you to use the official [MCP TypeScript SDK][mcp-ts] to
build and deploy [Gram Functions](https://getgram.ai).

[mcp-ts]: https://github.com/modelcontextprotocol/typescript-sdk

Use Gram Functions to build tools and resources for MCP servers. They can do
arbitrary tasks such as fetching data from APIs, performing calculations, or
interacting with hosted databases.

## Prerequisites

- [Node.js](https://nodejs.org) version 22.18.0 or later
- [Gram CLI](https://www.speakeasy.com/docs/gram/command-line/use)

## Quick start

To get started, install dependencies and run the development server:

```bash
pnpm install
```

To build a zip file that can be deployed to Gram, run:

```bash
pnpm build
```

After building, push your function to Gram with:

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
you interactively test your tools and resources.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
