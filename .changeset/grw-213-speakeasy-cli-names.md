---
"cli": minor
---

Use Speakeasy names for CLI settings and files. Every old name keeps working:

- Environment variables: `SPEAKEASY_AI_API_KEY`, `SPEAKEASY_AI_API_URL`, `SPEAKEASY_AI_SITE_URL`, `SPEAKEASY_AI_ORG`, `SPEAKEASY_AI_PROJECT`, `SPEAKEASY_AI_PROFILE`, `SPEAKEASY_AI_PROFILE_PATH`, `SPEAKEASY_AI_LOG_LEVEL`, `SPEAKEASY_AI_LOG_PRETTY` and `SPEAKEASY_AI_FUNCTIONS_SDK_VERSION` win over their `GRAM_*` names. When only a `GRAM_*` name is set, the CLI prints one deprecation line per run.
- Profiles: the default profile file is now `$XDG_CONFIG_HOME/speakeasy-ai/profile.json` (`~/.config/speakeasy-ai/profile.json`). Until it exists, the CLI reads `~/.gram/profile.json`, and the first save copies your profiles to the new file without deleting the old one. `speakeasy auth clear` empties both.
- Deployment file: `speakeasy stage` and `speakeasy push` default to `speakeasy.deploy.json` and use `gram.deploy.json` when only that file exists. `speakeasy push --config` is now optional.
- `speakeasy functions build` and `push` build with `@speakeasy-api/functions` when the project has it, then fall back to `@gram-ai/functions`. `speakeasy functions init` creates projects on `@speakeasy-api/functions` with a `src/functions.ts` entrypoint.
