---
"@speakeasy-api/functions": minor
"@gram-ai/functions": minor
"@gram-ai/create-function": patch
"cli": minor
"server": patch
---

Rename Gram Functions and the CLI's settings to Speakeasy names. Every old name keeps working as a deprecated alias.

- **SDK package:** the Functions SDK is published as `@speakeasy-api/functions`. Its main class is `Functions`, and the MCP helpers are `fromFunctions` and `withFunctions`. `Gram`, `fromGram` and `withGram` stay exported as deprecated aliases of the same objects. `@gram-ai/functions` depends on `@speakeasy-api/functions` at the same version and re-exports it, including the `/mcp` and `/build` subpaths and the `gf` command, so existing imports are unchanged. New projects from `speakeasy functions init` and `@gram-ai/create-function` use `@speakeasy-api/functions`.
- **Project files:** projects default to `speakeasy.config.*`, a `src/functions.ts` entrypoint and a `speakeasy.deploy.json` deployment file. `gram.config.*`, `src/gram.ts` and `gram.deploy.json` are still read when only they exist, with a one-line note for the old config and deployment file names. Builds write `dist/functions.zip` and remove a `dist/gram.zip` left by an older SDK. `speakeasy push --config` is now optional.
- **Environment variables:** `SPEAKEASY_AI_API_KEY`, `SPEAKEASY_AI_API_URL`, `SPEAKEASY_AI_SITE_URL`, `SPEAKEASY_AI_ORG`, `SPEAKEASY_AI_PROJECT`, `SPEAKEASY_AI_PROFILE`, `SPEAKEASY_AI_PROFILE_PATH`, `SPEAKEASY_AI_LOG_LEVEL`, `SPEAKEASY_AI_LOG_PRETTY`, `SPEAKEASY_AI_FUNCTIONS_SDK_VERSION`, `SPEAKEASY_AI_CLI_PATH` and `SPEAKEASY_AI_DEV` win over their `GRAM_*` names. When only a `GRAM_*` name is set, the CLI prints one deprecation line per run. Plain `SPEAKEASY_*` variables, which belong to the Speakeasy SDK generator, are never read. Functions that opt in to the caller's email receive it as `SPEAKEASY_AI_USER_EMAIL` as well as `GRAM_USER_EMAIL`.
- **CLI profile:** the default profile file is `$XDG_CONFIG_HOME/speakeasy-ai/profile.json` (`~/.config/speakeasy-ai/profile.json`). Until it exists, the CLI reads `~/.gram/profile.json`, and the first save copies your profiles across without deleting the old file. `speakeasy auth clear` empties both.
- **CLI text:** help, errors and prompts no longer mention Gram, and help lists only the `SPEAKEASY_AI_*` variable names.
