# Gram Functions x Resend Example

This Gram Function shows how to send emails using the Resend API. The function accepts an email address, subject, and message text, then sends an email and returns a success response with the email ID.

## Usage

- Sign up to [Resend](https://resend.com/) and create an API key
- Store your Resend API key as an environment variable in your Gram deployment:
  - `RESEND_API_KEY` - Your Resend API key
- Verify your domain in Resend (or use their test domain `onboarding@resend.dev` for development)
- You're all set to build and push this Gram Function!
  - Run `pnpm install && pnpm build && pnpm push`

## Quick start

To get started, install dependencies:

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
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
