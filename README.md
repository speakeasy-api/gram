<a href="https://www.speakeasy.com/product/gram" target="_blank">
   <picture>
       <source media="(prefers-color-scheme: light)" srcset="https://github.com/user-attachments/assets/1812f171-1650-4045-ac35-21bdd7b103a6">
       <source media="(prefers-color-scheme: dark)" srcset="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751">
       <img src="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751#gh-dark-mode-only" alt="Gram">
   </picture>
 </a>

<h3 align="center">Gram - The MCP Cloud Platform</h3>
<p align="center">
    <br />
    <a href="https://www.speakeasy.com/product/gram"><strong>Learn more »</strong></a>
    <br />
    <br />
    <a href="https://speakeasy.com/"><img alt="Built by Speakeasy" src="https://www.speakeasy.com/assets/badges/built-by-speakeasy.svg" />
    <br />
  </a>
    <a href="#Support"><strong>Documentation</strong></a> ·
    <a href="#Techstack"><strong>Tech Stack</strong></a> ·
    <a href="/CONTRIBUTING.md"><strong>Contributing</strong></a> ·
    <a href="https://app.getgram.ai/"><strong>Login</strong></a>
</p>

<p align="center">

</p>

<hr />

# Introduction

[Gram](https://app.getgram.ai) is a platform for creating, curating, and hosting Model Context Protocol (MCP) servers with ease. We currently support both OpenAPI documents as well as custom TypeScript functions as sources for tools.

## What can you do with Gram?

With Gram you can empower your LLM and Agents to access the right data at the right time. Gram provides a high-level TypeScript SDK and OpenAPI support to define tools, compose higher order custom tools and group tools together into toolsets. Every toolset is instantly available as a hosted and secure MCP server.

If you are looking to get started on the hosted platform you can [Sign up](https://app.getgram.ai/), or check out the [Quickstart guide](https://www.getgram.ai/docs/introduction).

## Features

└ Minimal, lightweight, and open source.  
└ High-level TypeScript framework that makes working with MCP easy.  
└ Use a custom tool builder to create higher-order tools by chaining lower level tools.  
└ OAuth support out-of-the-box: DCR, BYO Authorisation, and standard flows.  
└ First class support for OpenAPI `3.0.X` and `3.1.X`.  
└ Follows the [MCP](https://modelcontextprotocol.io/docs/getting-started/intro) specification.

## Gram Functions

Create agentic tools from simple TypeScript code using the [Gram Functions Framework](https://www.getgram.ai/docs/gram-functions/introduction). Refer to the [Getting Started](https://www.getgram.ai/docs/getting-started/typescript) guide to learn more.

The fastest way to get started is with the `npm create @gram-ai/function@latest` command, which creates a complete TypeScript project with a working Gram function. Deployable and runnable locally as a MCP server.

```bash
# Install the CLI and follow the prompts
npm create @gram-ai/function@latest

# Once created, move into your newly created function directory
cd my_function

# Build and Deploy
npm run build
npm run push
```

A default function is created for you.

```typescript
import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram().tool({
  name: "add",
  description: "Add two numbers together",
  inputSchema: { a: z.number(), b: z.number() },
  async execute(ctx, input) {
    return ctx.json({ sum: input.a + input.b });
  },
});

export default gram;
```

In addition you get a:

- A `server.ts` is created so you can run the tool locally as a MCP server with MCP inspector with `pnpm run dev`
- A `README` and `CONTRIBUTING` guide for next steps on building out your custom tool.

### Common use cases include:

└ Host one or more remote MCP servers at a custom domain like `mcp.{your-company}.com`.  
└ Power your in-application chat by exposing context from your internal APIs or 3rd Party APIs through tools.  
└ Add data to your AI workflows in Zapier, N8N and other workflow platforms  
└ Manage and secure MCP servers for your entire organization through a unified control plane.

Check out the `examples` folder in this repo for working examples. Or open a pull request if you have one to share!

## Gram CLI

The CLI allows for programmatic access to Gram, enabling you to manage the process of pushing sources (either OpenAPI documents or Gram Functions) for your MCP servers. Get started with documentation [here](https://www.getgram.ai/docs/command-line).

```bash
curl -fsSL https://go.getgram.ai/cli.sh | bash
```

And then:

```bash
gram auth
```

## Support

- Slack: [Join our slack](https://join.slack.com/t/speakeasy-dev/shared_invite/zt-3hudfoj4y-9EPqMmHIFhNiTtannqiV3Q) for support and discussions
- In-App: When using the [application](https://app.getgram.ai/) you can engage with the core maintainers of the product.
- GitHub: Contribute or report issues [on this repository](https://github.com/speakeasy-api/gram/issues/new).
- Documentation for Gram is also open source. View it [here](https://www.getgram.ai/docs/introduction) and contribute [here](https://github.com/speakeasy-api/developer-docs/tree/main/docs/gram).

## Contributing

Contributions are welcome! Please open an issue or discussion for questions or suggestions before starting significant work.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup and detailed contribution guidelines.

## Techstack

└ [TypeScript](https://www.typescriptlang.org/) – dashboard language.  
└ [Golang](https://go.dev/) - backend language.  
└ [Goa](https://github.com/goadesign/goa) - design-first API framework.  
└ [Temporal](https://temporal.io/) - workflow engine.  
└ [Polar](https://polar.sh/) - usage based billing.  
└ [OpenRouter](https://openrouter.ai/) - LLM gateway.  
└ [Speakeasy](https://www.speakeasy.com/) - Generated SDKs. Spec hosted [here](http://app.getgram.ai/openapi.yaml).
