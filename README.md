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
    <a href="#Contributing"><strong>Contributing</strong></a> ·
    <a href="https://app.getgram.ai/"><strong>Login</strong></a> ·
</p>

<p align="center">

</p>

<hr />

# Introduction

Gram is a platform for creating, curating, and hosting MCP servers. Curate and scope toolsets for every use case. Host and secure MCP servers with ease.

## What can you do with Gram?

With Gram you can empower your LLM and Agents to access the right data at the right time. Gram provides a high-level TypeScript SDK and OpenAPI support to define tools, compose higher order custom tools and group tools together into toolsets. Every toolset is instantly available as a hosted and secure MCP server.

If you are looking to get started on the hosted platform you can do that [here](https://app.getgram.ai/) or check out the [quickstart guide](https://www.speakeasy.com/docs/gram/introduction).

## Features

└ Minimal, lightweight, and open source.  
└ High-level TypeScript framework that makes working with MCP easy.  
└ Use a custom tool builder to create higher-order tools by chaining lower level tools.  
└ OAuth support out-of-the-box: DCR, BYO Authorisation, and standard flows.  
└ First class support for OpenAPI `3.0.X` and `3.1.X`.  
└ Follows the [MCP](https://modelcontextprotocol.io/docs/getting-started/intro) specification.

## Getting started with Gram Functions

Create agentic tools from simple TypeScript code using the [Gram Functions Framework](https://www.speakeasy.com/docs/gram/gram-functions/introduction). See the getting started [guide](https://www.speakeasy.com/docs/gram/getting-started/typescript) to learn more.

The fastest way to get started is with the `npm create @gram-ai/function@latest` command, which creates a complete TypeScript project with a working Gram function. Deployable and runnable locally as a MCP server.

```bash
# Install the CLI and Create a new project
npm create @gram-ai/function@latest

# Build and Deploy
npm run build
npm run push

# Check out your first function
cd my_function/src/gram.ts
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

## Common use cases include:

└ Host one or more remote MCP servers at a custom domain like `mcp.{your-company}.com`.  
└ Power your in-application chat by exposing context from your internal APIs or 3rd Party APIs through tools.  
└ Add data to your AI workflows in Zapier, N8N and other workflow platforms  
└ Manage and secure MCP servers for your entire organization through a unified control plane.

Check out the `examples` folder in this repo for working examples. Or open a pull request if you have one to share!

## `gram` CLI

The `gram` CLI a tool for programmatic access to Gram. Get started with documentation [here](https://docs.getgram.ai/command-line/installation).

### Local development

Quickstart:

```bash
cd cli
go run . --help
```

### Releases

> [!NOTE]  
> All CLI updates must follow the [changeset process](./docs/runbooks/version-management-with-changesets.md).

New versions of the CLI are released automatically with [GoReleaser](./.goreleaser.yaml).

Version bumps are determined by the git commit's prefix:

| Prefix   | Version bump | Example commit message                  |
| -------- | ------------ | --------------------------------------- |
| `feat!:` | Major        | `feat!: breaking change to deployments` |
| `feat:`  | Minor        | `feat: new status fields`               |
| `fix:`   | Patch        | `patch: update help docs`               |

## Support

- Slack: [Join our slack](https://join.slack.com/t/speakeasy-dev/shared_invite/zt-3hudfoj4y-9EPqMmHIFhNiTtannqiV3Q) for support and discussions
- In-App: When using the [application](https://app.getgram.ai/) you can engage with the core maintainers of the product.
- GitHub: Contribute or report issues [on this repository](https://github.com/speakeasy-api/gram/issues/new).
- Documentation for Gram is also open source. View it [here](https://www.speakeasy.com/docs/gram/introduction) and contribute [here](https://github.com/speakeasy-api/developer-docs/tree/main/docs/gram).

## Contributing

Contributions are welcome! Please open an issue or discussion for questions or suggestions before starting significant work!
Here's how you can develop on the stack and contribute to the project.

### Development

Run `./zero` until it succeeds. This script is what you will use to run the dashboard and services for local development. It will also handle installing dependencies and running pending database migrations before starting everything up.

The main dependencies for this project are Mise and Docker. The `./zero` script will guide you to install these if they are not found.

### Coding guidelines

All AI coding guidelines are written out in [CLAUDE.md](./CLAUDE.md). Please make sure you read the [contributing guidelines](./CONTRIBUTING.md) before submitting changes to this project.

### Putting up pull requests

Please have a good title and description for your PR. Go nuts with streams of commits but invest in a reviewable PR with good context.

## Techstack

└ [TypeScript](https://www.typescriptlang.org/) – dashboard language.  
└ [Golang](https://go.dev/) - backend language.  
└ [Goa](https://github.com/goadesign/goa) - design-first API framework.  
└ [Temporal](https://temporal.io/) - workflow engine.  
└ [Polar](https://polar.sh/) - usage based billing.  
└ [OpenRouter](https://openrouter.ai/) - LLM gateway.  
└ [Speakeasy](https://www.speakeasy.com/) - Generated SDKs. Spec hosted [here](http://app.getgram.ai/openapi.yaml).
