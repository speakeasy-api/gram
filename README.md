<a href="https://www.speakeasy.com/product/gram" target="_blank">
   <picture>
       <source media="(prefers-color-scheme: light)" srcset="https://github.com/user-attachments/assets/1812f171-1650-4045-ac35-21bdd7b103a6">
       <source media="(prefers-color-scheme: dark)" srcset="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751">
       <img src="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751#gh-dark-mode-only" alt="Gram">
   </picture>
 </a>

<h3 align="center">Gram</h3>

<p align="center">
    The platform to create, curate and host MCP servers.
    <br />
    <a href="https://www.speakeasy.com/product/gram"><strong>Learn more »</strong></a>
    <br />
    <br />
    <a href="https://speakeasy.com/"><img alt="Built by Speakeasy" src="https://www.speakeasy.com/assets/badges/built-by-speakeasy.svg" />
    <br />
  </a>
    <a href="#Documentation"><strong>Documentation</strong></a> ·
    <a href="#Techstack"><strong>Tech Stack</strong></a> ·
    <a href="#Contributing"><strong>Contributing</strong></a> ·
    <a href="https://app.getgram.ai/"><strong>Login</strong></a> ·
</p>

<p align="center">

</p>

<hr />

# Introduction

Gram is a platform for creating, curating, and hosting MCP servers. Create agent tools from your API that just work. Curate and scope toolsets for every use case. Host and secure MCP servers with ease.

With Gram you can:  
└ Host one or more remote MCP servers at `mcp.{your-company}.com`.  
└ Power agentic workflows or chat in your product by exposing context from your internal APIs to LLMs.    
└ Manage and secure MCP servers for your entire organization through a unified control plane.  

## Features

└ Minimal, lightweight, and open source.  
└ Multiple APIs: Mix and match any number of APIs into toolsets.  
└ Custom tool builder: Create higher-order tools by chaining existing endpoints.  
└ OAuth support out-of-the-box: DCR, BYO Authorisation, and standard flows.  
└ First class support for OpenAPI `3.0.X` and `3.1.X`.  
└ Follows the [MCP](https://modelcontextprotocol.io/docs/getting-started/intro) specification.

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

## `gram` (command line interface)

`gram` is a tool for programmatic access to Gram.

Get started at https://docs.getgram.ai/command-line/installation.

### Local development

Quickstart:

```bash
$ cd cli
$ go run . --help
```

### Releases

_Note: all CLI updates must follow the [changeset process](./docs/runbooks/version-management-with-changesets.md)._

New versions of the CLI are released automatically with GoReleaser ([.goreleaser.yaml](./.goreleaser.yaml)).

Version bumps are determined by the git commit's prefix:

| Prefix   | Version bump | Example commit message                  |
| -------- | ------------ | --------------------------------------- |
| `feat!:` | Major        | `feat!: breaking change to deployments` |
| `feat:`  | Minor        | `feat: new status fields`               |
| `fix:`   | Patch        | `patch: update help docs`               |

## Documentation

Documentation for Gram is also open source and can be found [here](https://docs.getgram.ai/).
