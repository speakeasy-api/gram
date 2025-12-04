# Gram Function MCP Template

This template allows you to use the official [MCP TypeScript SDK][mcp-ts] to
build and deploy [Gram Functions](https://getgram.ai).

[mcp-ts]: https://github.com/modelcontextprotocol/typescript-sdk

## Prerequisites

- [Node.js](https://nodejs.org) version 22.18.0 or later
- [Gram CLI](https://www.speakeasy.com/docs/gram/command-line/use)

## Quick start

This is specifically intended at deploying an OpenAI Apps SDK example Pizza Map through Gram Functions.

To get started, install dependencies:

```bash
pnpm install
```


Bundle all HTML, JS, and CSS content into a single `widget-template.ts` file. This bundles everything into your Gram function, making it easy to deploy without hosting assets separately:


```bash
pnpm inline:app
```


To build a zip file that can be deployed to Gram, run:

```bash
pnpm build
```

Ensure you are authenticated with your Gram account by running:

```bash
gram auth
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
