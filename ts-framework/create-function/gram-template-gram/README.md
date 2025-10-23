# Hello from Gram Functions!

This project builds and deploys [Gram Functions](https://getgram.ai) using a
tiny TypeScript framework that looks like this:

```ts
import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export default gram;
```

Gram Functions are tools for LLMs and MCP servers that can do arbitrary tasks
such as fetching data from APIs, performing calculations, or interacting with
hosted databases.

## Getting Started

To get started, install dependencies and run the development server:

```bash
pnpm install
```

To build a zip file that can be deployed to Gram, run:

```bash
pnpm build
```

## Testing Locally

If you want to poke at the tools you've built during local development, you can
start a Hono server with:

```bash
pnpm dev
```

Now you can simulate tool calls with:

```bash
curl \
  --data '{"name": "greet", "input": {"name": "Georges"}}' \
  -H "Content-Type: application/json" \
  http://localhost:3000/tool-call
```

## What next?

To learn more about using the framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md)
