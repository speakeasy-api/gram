---
"@speakeasy-api/functions": minor
"@gram-ai/functions": minor
"@gram-ai/create-function": patch
---

Publish the Functions SDK as `@speakeasy-api/functions`. Its main class is now `Functions`, and the MCP helpers are `fromFunctions` and `withFunctions`. `Gram`, `fromGram` and `withGram` stay exported as deprecated aliases of the same objects.

`@gram-ai/functions` keeps working: it depends on `@speakeasy-api/functions` at the same version and re-exports it, including the `/mcp` and `/build` subpaths and the `gf` command.

Projects now default to `speakeasy.config.*`, the `src/functions.ts` entrypoint and the `speakeasy.deploy.json` deployment file. The build still reads `gram.config.*`, `src/gram.ts` and `gram.deploy.json` when only those exist, and prints a one-line note for the old config and deployment file names. The build now writes `dist/functions.zip` and removes a `dist/gram.zip` left by an older SDK. `SPEAKEASY_AI_CLI_PATH` and `SPEAKEASY_AI_DEV` replace `GRAM_CLI_PATH` and `GRAM_DEV`, which still work.

New projects from `@gram-ai/create-function` depend on `@speakeasy-api/functions`.
