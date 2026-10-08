# Speakeasy Functions MCP Template

This template allows you to use the official [MCP TypeScript SDK][mcp-ts] to
build and deploy [Speakeasy Functions](https://www.speakeasy.com).

[mcp-ts]: https://github.com/modelcontextprotocol/typescript-sdk

Use Speakeasy Functions to build tools and resources for MCP servers. They can do
arbitrary tasks such as fetching data from APIs, performing calculations, or
interacting with hosted databases.

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
you interactively test your tools and resources.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
