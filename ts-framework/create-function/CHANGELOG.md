# @gram-ai/create-function

## 0.2.1

### Patch Changes

- a8cc73e: Ensure .gitignore is scaffolded out with templating Gram Functions projects.

## 0.2.0

### Minor Changes

- baaa388: Add a simple Hono-based server to the Gram Functions mini-framework template.
  This server allows developers to run and test their Gram Functions locally
  before deploying them.

## 0.1.1

### Patch Changes

- 8675163: Adjust fs.cp filter so scaffolding works

## 0.1.0

### Minor Changes

- 9c386f1: Introducing a new framework that simplifies writing and bundling Gram Functions.
  The simplest "Hello, World!" Gram Function with this framework looks like this:

  ```typescript
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

  In addition to the new framework, we introduce a project scaffolder that users
  can run to quickly set up a new Gram Functions project with all necessary
  boilerplate and configuration. When it is published, it will be available
  through:

  ```
  pnpm create @gram-ai/function
  ```
