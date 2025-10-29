# @gram-ai/functions

## 0.5.3

### Patch Changes

- 2155915: Removed `import.meta.url` check for bin scripts. The value for that meta
  property is not resolving to be equal to process.argv[1] when running the
  compiled JavaScript files as bin scripts.

## 0.5.2

### Patch Changes

- abcbfd9: Switched away from `import.meta.main` to `import.meta.url`. The former approach
  is supported primarily by Deno and Bun and only gained experimental support in
  Node.js 22.18.0. To ensure broader compatibility across different Node.js
  versions, we replace these checks with a more traditional method that compares
  `import.meta.url` to the script's file URL derived from `process.argv[1]`.

## 0.5.1

### Patch Changes

- 5ea5fcd: Reverted the Gram Functions TS framework template to export the instance of
  `Gram` as the default export in `src/gram.ts`. This makes the boilerplate code
  work again when deployed.
- 15cbf7e: Allows for creating functions with variables in gram functions built with mcp passthrough

## 0.5.0

## 0.4.0

### Minor Changes

- 3fc0efc: allow for defining resources in mcp builds of gram functions
- add1481: Removed the per-tool config for declaring environment variables and instead opts
  for updating the Gram class to optionally accept an input environment and an
  associated zod schema for it. When a schema is defined, the code benefits from
  strict types and transforms when accessing environment variables via the tool
  context.

## 0.3.2

### Patch Changes

- a609f61: Added an "engines" field to the package.json files of the `@gram-ai/functions`
  requiring Node.js version 22.18.0 or higher. This ensures that we are in a
  runtime that supports import assertions and native support for running
  TypeScript files without experimental flags.

## 0.3.1

### Patch Changes

- cd9df97: Added the missing 'zx' dependency to the Gram TS SDK's `package.json`. It is
  needed for the build process.

## 0.3.0

### Minor Changes

- 8fa3809: Updated the Gram Functions TypeScript SDK and the Gram Functions template to
  support seamless build and deploy powered by the SDK and Gram CLI.

## 0.2.1

### Patch Changes

- 676405c: Updated the `manifest()` method of the Gram Functions TS framework to avoid
  JSON-serializing the input schema for tool definitions. This was a mistake since
  the server is expecting a literal object for the schema.

## 0.2.0

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
