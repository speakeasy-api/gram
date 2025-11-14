# Gram Functions: Basic API Integration

This example demonstrates real-world patterns for building [Gram
Functions](https://www.speakeasy.com/docs/gram/gram-functions/introduction) that
integrate with external APIs. Study this project to learn how to structure your
own LLM tools with proper error handling, validation, and data transformation.

## What's Included

**Simple Tool (`get_last_n_launches`)**

Fetches recent SpaceX launches. Demonstrates:

- Basic API integration
- Optional parameters with validation
- Error handling patterns

**Complex Tool (`get_launch_weather`)**

Retrieves historical weather for SpaceX launches. Demonstrates:

- Multi-step API orchestration (SpaceX → launchpad → weather)
- Data transformation across services
- Coordinating multiple external dependencies

## Key Patterns

- **Type-safe validation** with Zod schemas
- **Error handling** with `ctx.fail()`
- **Modular architecture** separating tool definitions from API clients
- **Real API integration** showing production-ready patterns

## Quick start

To get started, install dependencies and run the development server:

```bash
npm install
```

To build a zip file that can be deployed to Gram, run:

```bash
npm build
```

After building, push your function to Gram with:

```bash
npm push
```

## Testing Locally

If you want to poke at the tools you've built during local development, you can
start a local MCP server over stdio transport with:

```bash
npm dev
```

Specifically, this command will spin up [MCP inspector][mcp-inspector] to let
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector

## Learn More

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions/introduction)
- [Framework API Reference](./CONTRIBUTING.md)
