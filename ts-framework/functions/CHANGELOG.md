# @gram-ai/functions

## 0.12.4

### Patch Changes

- 84736c7: Support tool annotations in functions framework. Adds `ToolAnnotations` type allowing function authors to specify annotations via `Gram.tool({ annotations: { ... } })`

## 0.12.3

### Patch Changes

- 0420d41: Treat non-JSON log lines from underlying Gram golang CLI as regular info level logs instead of errors.

## 0.12.2

## 0.12.1

## 0.12.0

### Minor Changes

- 3ff96b9: Allows the specification of a "oauth variable", into which an oauth token will be piped if available

## 0.11.2

### Patch Changes

- bc147e0: Updated dependencies to address dependabot security alerts

## 0.11.1

### Patch Changes

- 13b76b8: Fixes a regression where process.env was being ignored

## 0.11.0

### Minor Changes

- d5f4e35: Fix type EnvSchema type mismatch between fromGram and the Gram class

## 0.10.0

### Minor Changes

- b714f43: Gram instances are now composable. This allows some similarity to Hono's grouping pattern, making it possible to split and organize Gram Functions code bases more than before. For example, before:

  ```typescript
  const gram = new Gram({
    envSchema: {
      TRAIN_API_KEY: z.string().describe("API key for the train service"),
      FLIGHT_API_KEY: z.string().describe("API key for the flight service"),
    },
  })
    .tool({
      name: "train_book",
      description: "Books a train ticket",
    })
    .tool({
      name: "train_status",
      description: "Gets the status of a train",
    })
    .tool({
      name: "flight_book",
      description: "Books a flight ticket",
    })
    .tool({
      name: "flight_status",
      description: "Gets the status of a flight",
    });
  ```

  And now, with composibility:

  ```typescript
  // train.ts
  const trainGram = new Gram({
    envSchema: {
      TRAIN_API_KEY: z.string().describe("API key for the train service"),
    },
  })
    .tool({
      name: "train_book",
      description: "Books a train ticket",
    })
    .tool({
      name: "train_status",
      description: "Gets the status of a train",
    });
  })

  // flight.ts
  const flightGram = new Gram({
    envSchema: {
      FLIGHT_API_KEY: z.string().describe("API key for the flight service"),
    },
  })
    .tool({
      name: "flight_book",
      description: "Books a flight ticket",
    })
    .tool({
      name: "flight_status",
      description: "Gets the status of a flight",
    });

  // index.ts
  import { trainGram } from './train'
  import { flightGram } from './flight'

  const travelGram = new Gram()
    .extend(trainGram)
    .extend(flightGram);

  ```

## 0.9.2

### Patch Changes

- 2cc9008: Update functions cli to better track long deployments.

## 0.9.1

### Patch Changes

- 5b778e8: Updated esbuild config for Gram Functions to default to emitting a CommonJS require shim, allowing dynamic require() calls to work in bundled code. This is necessary for compatibility with certain dependencies that use dynamic requires.

## 0.9.0

### Patch Changes

- eeecc96: Deployment loader animation shown during `npm run push` while deployment asset is being uploaded.
- 5d65184: Adds markdown() response helper to the functions ctx

## 0.8.1

## 0.8.0

### Minor Changes

- f725a4c: Added a `fromGram` utility to the Gram Functions TypeScript SDK that converts an
  instance of the `Gram` mini-framework into an MCP server. This reduces the
  amount of boilerplate we emit in new projects that use the `gram-template-gram`
  template.

## 0.7.0

### Minor Changes

- a0853fe: Updated the Gram Functions SDK to separate the build and push commands. The
  `gram-build` has also been renamed to `gf` which now results in the commands
  `pnpm gf build` and `pnpm gf push`.
- 73e9c42: Renamed the MCP wrapper utility from `wrap` to `withGram` and adds TypeScript
  docs to various APIs in the Gram Functions SDK.

## 0.6.2

### Patch Changes

- f79fd52: Open dashboard from gram-build, better completing the flow starting from pnpm create

## 0.6.1

## 0.6.0

### Minor Changes

- 5a3f14c: Updated the MCP template and Gram Functions SDK to support building and
  deploying MCP servers directly through Gram. It removes extraneous build scripts
  and dependencies, simplifying the process for developers.

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
