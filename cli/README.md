# Speakeasy AI Control Plane CLI

The `speakeasy-ai` command line interface for the Speakeasy AI Control Plane.

## Install

```bash
# Homebrew (macOS and Linux)
brew install speakeasy-api/tap/cli

# npm
npm i -g @speakeasy-api/cli

# Install script (macOS and Linux)
curl -fsSL https://ai.speakeasy.com/cli.sh | bash
```

Check the install. It prints `speakeasy-ai-control-plane-cli`:

```bash
speakeasy-ai --control-plane-cli
```

Then authenticate and check your setup:

```bash
speakeasy-ai auth
speakeasy-ai whoami
```

### Migrating from `speakeasy`

This CLI used to install as `speakeasy`, the name the Speakeasy SDK generator
uses. It now installs as `speakeasy-ai`, so both CLIs can be on your `PATH`, and
the Homebrew `cli` formula no longer conflicts with the SDK generator formula
(`speakeasy-api/tap/speakeasy`).

- Homebrew and npm: upgrade as usual. The upgrade replaces `speakeasy` with
  `speakeasy-ai`.
- Functions projects: their `package.json` scripts call the CLI by name. Replace
  `speakeasy functions` with `speakeasy-ai functions` in them.
- Install script: run it again. It installs `speakeasy-ai`, and warns when an
  old `speakeasy` copy of this CLI is in the same folder. Remove old copies in
  other folders yourself.
- A copy updated to this release and still run as `speakeasy` prints a
  deprecation notice. Running an SDK generator command such as `speakeasy run`
  with it says to install `speakeasy-ai` and remove the old copy.

### Migrating from `gram`

This CLI was previously named `gram`. The `speakeasy-api/tap/gram` Homebrew
formula still installs it under the `gram` name for now, and running it as
`gram` prints a deprecation notice. Switch to the `speakeasy-ai` command.

You do not need to authenticate again. Profiles now live in
`$XDG_CONFIG_HOME/speakeasy-ai/profile.json` (`~/.config/speakeasy-ai/profile.json`
when `XDG_CONFIG_HOME` is unset). Until that file exists, the CLI reads your
old `~/.gram/profile.json`, and the first command that saves a profile copies
your profiles to the new file. The old file is left in place.
`speakeasy-ai auth clear` empties both files.

Environment variables and files now use Speakeasy names. The old names keep
working, and the CLI prints one deprecation line per run when you still use
an old environment variable:

| Old name                     | New name                             |
| ---------------------------- | ------------------------------------ |
| `GRAM_API_KEY`               | `SPEAKEASY_AI_API_KEY`               |
| `GRAM_API_URL`               | `SPEAKEASY_AI_API_URL`               |
| `GRAM_SITE_URL`              | `SPEAKEASY_AI_SITE_URL`              |
| `GRAM_ORG`                   | `SPEAKEASY_AI_ORG`                   |
| `GRAM_PROJECT`               | `SPEAKEASY_AI_PROJECT`               |
| `GRAM_PROFILE`               | `SPEAKEASY_AI_PROFILE`               |
| `GRAM_PROFILE_PATH`          | `SPEAKEASY_AI_PROFILE_PATH`          |
| `GRAM_LOG_LEVEL`             | `SPEAKEASY_AI_LOG_LEVEL`             |
| `GRAM_LOG_PRETTY`            | `SPEAKEASY_AI_LOG_PRETTY`            |
| `GRAM_FUNCTIONS_SDK_VERSION` | `SPEAKEASY_AI_FUNCTIONS_SDK_VERSION` |
| `gram.deploy.json`           | `speakeasy.deploy.json`              |

When both names of a variable are set, the `SPEAKEASY_AI_*` name wins. The CLI
never reads plain `SPEAKEASY_*` names such as `SPEAKEASY_API_KEY`, which belong
to the Speakeasy SDK generator CLI. `speakeasy-ai stage` and `speakeasy-ai push` use
`speakeasy.deploy.json` by default, and keep using `gram.deploy.json` when only
that file exists.

## Speakeasy Functions

The `speakeasy-ai functions` commands create, build and deploy
[Speakeasy Functions](../ts-framework/functions/README.md) projects:

| Command                             | What it does                                                                                                                                                 |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `speakeasy-ai functions init [dir]` | Create a project from a built-in template (`--template functions` or `--template mcp`). Prompts for missing values on a terminal; `--yes` uses the defaults. |
| `speakeasy-ai functions build`      | Build the project with its own `@speakeasy-api/functions` (or `@gram-ai/functions`) package into `dist/functions.zip`. Needs Node.js 22.18.0 or later.       |
| `speakeasy-ai functions dev`        | Run the project's `dev` script. Arguments after `--` are passed to it.                                                                                       |
| `speakeasy-ai functions push`       | Build, stage and deploy the project. `--no-build` deploys the existing build.                                                                                |
| `speakeasy-ai functions stage`      | Add a built zip file to the deployment file without deploying, like `speakeasy-ai stage function`.                                                           |

They replace `npm create @gram-ai/function` and the `gf build` and `gf push`
commands from `@gram-ai/functions`, which still work but are deprecated.

The build uses `@speakeasy-api/functions` when the project has it and falls
back to the deprecated `@gram-ai/functions`, so existing projects build
unchanged. The SDK reads `speakeasy.config.{ts,mts,js,mjs}`, then the
deprecated `gram.config.*`, and builds `src/functions.ts`, or `src/gram.ts`
when only that file exists.

The project templates live in
[`internal/functions/templates`](internal/functions/templates) and are embedded
in the binary. `@gram-ai/create-function` copies them from there when it is
built, so both scaffolders create the same projects.

To scaffold a project against a local SDK checkout, set
`SPEAKEASY_AI_FUNCTIONS_SDK_VERSION=file:/path/to/gram/ts-framework/functions`
when you run `speakeasy-ai functions init`.

## Local Development

1. Setup environment
   - `export SPEAKEASY_AI_API_URL=https://localhost:8080`
   - `export SPEAKEASY_AI_SITE_URL=https://localhost:5173`
   - `export SPEAKEASY_AI_ORG=organization-123`
   - `export SPEAKEASY_AI_PROJECT=default`
   - `export SPEAKEASY_AI_API_KEY=<API-KEY>`

2. Run desired command
   - `cd cli`
   - `go run main.go status`

### Testing Speakeasy Functions

1. Stage zip
   - `go run main.go functions stage --slug test-fn --location fixtures/example.zip`
   - _You never need to do this again as long as you are using the same zip_

2. Push
   - `go run main.go push --config speakeasy.deploy.json`
