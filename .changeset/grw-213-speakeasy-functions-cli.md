---
"cli": minor
---

Add `speakeasy functions` commands for Gram Functions projects:

- `speakeasy functions init [dir]` creates a project from a built-in template (`--template functions` or `--template mcp`). It prompts for missing values on a terminal, and `--yes` uses the defaults. `--git` and `--install` control git init and the dependency install with the detected package manager.
- `speakeasy functions build` builds the project with the `@gram-ai/functions` version it depends on. It needs Node.js 22.18.0 or later and `@gram-ai/functions` 0.19.0 or later, and says what to install when either is missing.
- `speakeasy functions dev` runs the project's `dev` script and passes arguments after `--` to it.
- `speakeasy functions push` builds, stages and deploys the project with your `speakeasy auth` profile. `--slug`, `--project`, `--scale` and `--memory-mib` override the project config, and `--no-build` deploys the existing build.
- `speakeasy functions stage` adds a built zip file to the deployment file without deploying, the same as `speakeasy stage function`, which keeps working.
