# Speakeasy AI Control Plane CLI

The `speakeasy` command line interface for the Speakeasy AI Control Plane.

## Install

```bash
# Homebrew (macOS and Linux)
brew install speakeasy-api/tap/cli

# npm
npm i -g @speakeasy-api/cli
```

Then authenticate and check your setup:

```bash
speakeasy auth
speakeasy whoami
```

The Homebrew `cli` formula conflicts with the Speakeasy SDK generator formula
(`speakeasy-api/tap/speakeasy`), because both install a binary named
`speakeasy`. Install one or the other with Homebrew.

### Migrating from `gram`

This CLI was previously named `gram`. The `speakeasy-api/tap/gram` Homebrew
formula still installs it under the `gram` name for now, and running it as
`gram` prints a deprecation notice. Switch to the `speakeasy` command.
Profiles stay in `~/.gram/profile.json`, so you do not need to authenticate
again.

## Gram Functions

The `speakeasy functions` commands create, build and deploy
[Gram Functions](../ts-framework/functions/README.md) projects:

| Command                          | What it does                                                                                                                                                 |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `speakeasy functions init [dir]` | Create a project from a built-in template (`--template functions` or `--template mcp`). Prompts for missing values on a terminal; `--yes` uses the defaults. |
| `speakeasy functions build`      | Build the project with its own `@gram-ai/functions` package into `dist/gram.zip`. Needs Node.js 22.18.0 or later.                                            |
| `speakeasy functions dev`        | Run the project's `dev` script. Arguments after `--` are passed to it.                                                                                       |
| `speakeasy functions push`       | Build, stage and deploy the project. `--no-build` deploys the existing build.                                                                                |
| `speakeasy functions stage`      | Add a built zip file to the deployment file without deploying, like `speakeasy stage function`.                                                              |

They replace `npm create @gram-ai/function` and the `gf build` and `gf push`
commands from `@gram-ai/functions`, which still work but are deprecated.

The project templates live in
[`internal/functions/templates`](internal/functions/templates) and are embedded
in the binary. `@gram-ai/create-function` copies them from there when it is
built, so both scaffolders create the same projects.

To scaffold a project against a local SDK checkout, set
`GRAM_FUNCTIONS_SDK_VERSION=file:/path/to/gram/ts-framework/functions` when you
run `speakeasy functions init`.

## Local Development

1. Setup environment
   - `export GRAM_API_URL=https://localhost:8080`
   - `export GRAM_DASHBOARD_URL=https://localhost:5173`
   - `export GRAM_ORG=organization-123`
   - `export GRAM_PROJECT=default`
   - `export GRAM_API_KEY=<API-KEY>`

2. Run desired command
   - `cd cli`
   - `go run main.go status`

### Testing Gram Functions

1. Stage zip
   - `go run main.go functions stage --slug test-fn --location fixtures/example.zip`
   - _You never need to do this again as long as you are using the same zip_

2. Push
   - `go run main.go push --config gram.deploy.json`
