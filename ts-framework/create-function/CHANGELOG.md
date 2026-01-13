# @gram-ai/create-function

## 0.12.1

### Patch Changes

- 095b298: Add a fallback for when symlink creation fails when creating gram functions

## 0.12.0

## 0.11.2

### Patch Changes

- bc147e0: Updated dependencies to address dependabot security alerts

## 0.11.1

## 0.11.0

## 0.10.0

## 0.9.2

## 0.9.1

### Patch Changes

- 0e27fff: Fix various aspects of the copy in create-function
- 5b778e8: Updated esbuild config for Gram Functions to default to emitting a CommonJS require shim, allowing dynamic require() calls to work in bundled code. This is necessary for compatibility with certain dependencies that use dynamic requires.

## 0.9.0

### Minor Changes

- bc44bf0: Enables hot reloading in dev for function templates

### Patch Changes

- 5d65184: Adds markdown() response helper to the functions ctx

## 0.8.1

### Patch Changes

- 8be35aa: Updated the scaffolding of Gram Functions projects to exclude the CHANGELOG.md file.
- 8be35aa: Fixed an issue where Gram Functions templates were not enforcing a minimum
  Node.js version due to a typo in the generated package.json files. The field was
  incorrectly named "engine" instead of "engines".
- 8be35aa: Ensured that templated Gram Function TS projects start at version "0.0.0" in
  their package.json instead of inheriting the version from the template

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

## 0.6.2

### Patch Changes

- 5f0f33b: Increase prescriptiveness of the hints for framework selection

## 0.6.1

### Patch Changes

- efe0388: Added a shebang line to the `create-function/src/main.ts` file, enabling it to
  be executed directly as a script.

## 0.6.0

### Minor Changes

- 5a3f14c: Updated the MCP template and Gram Functions SDK to support building and
  deploying MCP servers directly through Gram. It removes extraneous build scripts
  and dependencies, simplifying the process for developers.

## 0.5.3

## 0.5.2

## 0.5.1

### Patch Changes

- cfc187f: We now install the CLI and run gram auth automatically (if yes is chosen) during pnpm create

## 0.5.0

### Minor Changes

- 24118c5: Updated the Gram Functions framework template to include an MCP server for local
  development that wraps a `Gram` instance. This will provide developers a better
  feedback loop when developing tools before deploying them to Gram.

## 0.4.0

## 0.3.2

## 0.3.1

## 0.3.0

### Minor Changes

- 8fa3809: Updated the Gram Functions TypeScript SDK and the Gram Functions template to
  support seamless build and deploy powered by the SDK and Gram CLI.

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
