# Speakeasy Function MCP Template

This template allows you to use the official [MCP TypeScript SDK][mcp-ts] to
build and deploy [Speakeasy Functions](https://www.speakeasy.com/docs/ai-control-plane/mcp-gateway/building-servers/functions).

[mcp-ts]: https://github.com/modelcontextprotocol/typescript-sdk

## Prerequisites

- [Node.js](https://nodejs.org) version 22.18.0 or later
- [Speakeasy CLI](https://www.speakeasy.com/docs/ai-control-plane/reference/command-line)
  (`brew install speakeasy-api/tap/cli` or `npm i -g @speakeasy-api/cli`)

## Quick start

This template builds and deploys the OpenAI Apps SDK Pizza Map example through Speakeasy Functions.

To get started, install dependencies:

```bash
pnpm install
```

Bundle all HTML, JS, and CSS content into a single `widget-template.ts` file. This bundles everything into your function, making it easy to deploy without hosting assets separately:

```bash
pnpm inline:app
```

To build a zip file that can be deployed to Speakeasy, run:

```bash
pnpm build
```

Ensure you are authenticated with your Speakeasy account by running:

```bash
speakeasy auth
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
you interactively test your tools and resources.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
