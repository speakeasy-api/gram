<p align="center">
  <a href="https://www.speakeasy.com/product/gram" target="_blank">
    <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/speakeasy-icon.png" alt="Gram by Speakeasy" width="140">
  </a>
</p>

<h3 align="center">Speakeasy AI Control Plane</h3>

<p align="center">
  <strong>Securely scale AI usage across your organization. Built for humans and agents.</strong>
  <br />
  <a href="https://www.speakeasy.com/"><strong>Learn more »</strong></a>
</p>

<p align="center">
  <a href="https://www.getgram.ai/docs/introduction"><strong>Documentation</strong></a> ·
  <a href="#running-locally"><strong>Running locally</strong></a> ·
  <a href="#tech-stack"><strong>Tech Stack</strong></a> ·
  <a href="./CONTRIBUTING.md"><strong>Contributing</strong></a> ·
  <a href="https://app.getgram.ai/"><strong>Login</strong></a> ·
  <a href="https://roadmap.speakeasy.com/"><strong>Roadmap</strong></a>
</p>

# Introduction

Gram is the open source stack behind Speakeasy's AI control plane. Secure and centrally manage MCPs, Skills, and Assistants your whole company to access, with fine-grained permissions, threat detection, and full observability of token use and costs. Every tool call, permission change, and access event logged and searchable. SOC 2 Type II and ISO 27001 certified.

To get started on the hosted platform you can [Sign up](https://app.getgram.ai/), or check out the [Quickstart guide](https://www.getgram.ai/docs/introduction).

### Supports popular AI providers

<p align="center">
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/claude.svg" alt="Claude" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/claude-code.svg" alt="Claude Code" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/openai.svg" alt="ChatGPT" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/codex.svg" alt="Codex" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/gemini.png" alt="Gemini" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/cursor.svg" alt="Cursor" height="40">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="https://raw.githubusercontent.com/speakeasy-api/gram/main/.github/agent-icons/gh-copilot.svg" alt="GitHub Copilot" height="40">
</p>

## Observe

Track AI usage across teams and measure impact with either tokens or cost. Deep dive expensive sessions, create budgets and measure tool effectiveness. Built on a foundation of Opentelemetry. Exportable and interactive via platform MCP and a built in assistant.

## Secure

Every prompt, response, and agent action is inspected and enforced in real time. Sensitive data is blocked, redacted, or logged before it leaves your environment. Create and enforce flexible policies to prevent prompt injection, log shadow use and detect PII secrets and other sensitive data types.

## Connect

A single control layer for connecting your agents to MCPs for your SaaS vendors, APIs, and internal systems, with policy enforcement and granular access control built in. Private networking and tunneling deployed on demand.

## Distribute

Centralise distribution of MCPs, Skills, Plugins and Assistants to your team based on enterprise roles. Team, server, and tool level permissions enforced through RBAC and Oauth2.1. Synced to your enterprise IDP (Okta, Azure AD, Google Workspace, etc.).

## Support

- Chat with us: [Join our slack](https://join.slack.com/t/speakeasy-dev/shared_invite/zt-3hudfoj4y-9EPqMmHIFhNiTtannqiV3Q) for support and discussions or email us at [support@speakeasy.com](mailto:support@speakeasy.com).
- Contribute feature requests or report issues [on our roadmap](https://roadmap.speakeasy.com/).
- Documentation for the platform is available [here](https://www.speakeasy.com/docs/mcp).

## Running locally

Run `./zero` until it succeeds. This script is what you use to run the dashboard and services for local development. It installs dependencies, runs pending database migrations, and starts everything up.

The main dependencies are [Mise](https://mise.jdx.dev/) and [Docker](https://www.docker.com/). The `./zero` script will guide you to install these if they are not found.

Once everything is running, seed the local database with sample data:

```bash
mise seed
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for more detail on local development, auth, and the CLI.

## Contributing

Contributions are welcome! Please open an issue or discussion for questions or suggestions before starting significant work.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup and detailed contribution guidelines.

## Tech Stack

- [TypeScript](https://www.typescriptlang.org/) — dashboard language.
- [Golang](https://go.dev/) — backend language.
- [Goa](https://github.com/goadesign/goa) — design-first API framework.
- [Temporal](https://temporal.io/) — workflow engine.
- [Polar](https://polar.sh/) — usage-based billing.
- [OpenRouter](https://openrouter.ai/) — LLM gateway.
- [Speakeasy](https://www.speakeasy.com/) — generated SDKs. Spec hosted [here](https://app.getgram.ai/openapi.yaml).

## Contributors

Thanks to everyone who has contributed to Gram!

<a href="https://github.com/speakeasy-api/gram/graphs/contributors">
  <img alt="Gram contributors" src="https://contrib.rocks/image?repo=speakeasy-api/gram" />
</a>

<hr />
<br />

<p align="left">
  <a href="https://speakeasy.com/"><img alt="Built by Speakeasy" src="https://www.speakeasy.com/assets/badges/built-by-speakeasy.svg" /></a>
</p>
