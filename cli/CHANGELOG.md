# cli

## 0.19.0

### Minor Changes

- 9a02108: Rename Gram Functions and the CLI's settings to Speakeasy names. Every old name keeps working as a deprecated alias.
  
  - **SDK package:** the Functions SDK is published as `@speakeasy-api/functions`. Its main class is `Functions`, and the MCP helpers are `fromFunctions` and `withFunctions`. `Gram`, `fromGram` and `withGram` stay exported as deprecated aliases of the same objects. `@gram-ai/functions` depends on `@speakeasy-api/functions` at the same version and re-exports it, including the `/mcp` and `/build` subpaths and the `gf` command, so existing imports are unchanged. New projects from `speakeasy functions init` and `@gram-ai/create-function` use `@speakeasy-api/functions`. The `@gram-ai/create-function` template is now `--template functions`, and `--template gram` still works.
  - **Project files:** projects default to `speakeasy.config.*`, a `src/functions.ts` entrypoint and a `speakeasy.deploy.json` deployment file. `gram.config.*`, `src/gram.ts` and `gram.deploy.json` are still read when only they exist, with a one-line note for the old config and deployment file names. Builds write `dist/functions.zip` and remove a `dist/gram.zip` left by an older SDK. `speakeasy push --config` is now optional.
  - **Environment variables:** `SPEAKEASY_AI_API_KEY`, `SPEAKEASY_AI_API_URL`, `SPEAKEASY_AI_SITE_URL`, `SPEAKEASY_AI_ORG`, `SPEAKEASY_AI_PROJECT`, `SPEAKEASY_AI_PROFILE`, `SPEAKEASY_AI_PROFILE_PATH`, `SPEAKEASY_AI_LOG_LEVEL`, `SPEAKEASY_AI_LOG_PRETTY` and `SPEAKEASY_AI_FUNCTIONS_SDK_VERSION` win over their `GRAM_*` names in the CLI, which prints one deprecation line per run when only a `GRAM_*` name is set. The SDK reads `SPEAKEASY_AI_CLI_PATH` and `SPEAKEASY_AI_DEV` before `GRAM_CLI_PATH` and `GRAM_DEV`. Plain `SPEAKEASY_*` variables, which belong to the Speakeasy SDK generator, are never read. Functions that opt in to the caller's email receive it as `SPEAKEASY_AI_USER_EMAIL` as well as `GRAM_USER_EMAIL`.
  - **CLI profile:** the default profile file is `$XDG_CONFIG_HOME/speakeasy-ai/profile.json` (`~/.config/speakeasy-ai/profile.json`). Until it exists, the CLI reads `~/.gram/profile.json`, and the first save copies your profiles across without deleting the old file. `speakeasy auth clear` empties both.
  - **CLI text:** help, errors and prompts no longer mention Gram, and help lists only the `SPEAKEASY_AI_*` variable names.

## 0.18.0

### Minor Changes

- 261c304: Rename the CLI command to `speakeasy`. Install it with `brew install speakeasy-api/tap/cli` or `npm i -g @speakeasy-api/cli`. The `speakeasy-api/tap/gram` Homebrew formula keeps installing the CLI as `gram` for now; running it as `gram` prints a deprecation notice and otherwise works as before. Profiles stay in `~/.gram/profile.json`.
- 261c304: Add `speakeasy functions` commands for Gram Functions projects:
  
  - `speakeasy functions init [dir]` creates a project from a built-in template (`--template functions` or `--template mcp`). It prompts for missing values on a terminal, and `--yes` uses the defaults. `--git` and `--install` control git init and the dependency install with the detected package manager.
  - `speakeasy functions build` builds the project with the `@gram-ai/functions` version it depends on. It needs Node.js 22.18.0 or later and `@gram-ai/functions` 0.19.0 or later, and says what to install when either is missing.
  - `speakeasy functions dev` runs the project's `dev` script and passes arguments after `--` to it.
  - `speakeasy functions push` builds, stages and deploys the project with your `speakeasy auth` profile. `--slug`, `--project`, `--scale` and `--memory-mib` override the project config, and `--no-build` deploys the existing build.
  - `speakeasy functions stage` adds a built zip file to the deployment file without deploying, the same as `speakeasy stage function`, which keeps working.
  
  `speakeasy push` now deploys to the API URL saved by `speakeasy auth` when neither `--api-url` nor `GRAM_API_URL` is set, instead of always using `https://app.getgram.ai`.

## 0.17.0

### Minor Changes

- 1cad72d: Add ChatGPT Desktop as an MCP client on the hosted install page and in
  `gram install chatgpt-desktop`, with Developer mode and custom connector steps
  matched to the server's authentication.

## 0.16.0

### Minor Changes

- 42e4248: Add support for scaling the number of instances and memory for machines deployed for a Gram Function. It is now possible to go up to 5 machines per function and up to 4096 MiB for each machine.

## 0.15.8

### Patch Changes

- 70d8ad3: Fix CLI release signing by switching to cosign v3 bundle format and migrating to ubuntu runner

## 0.15.7

### Patch Changes

- 569cbe2: fix cli update when using homebrew

## 0.15.6

### Patch Changes

- cfd28c6: Removed the automatic opening of the deployment logs URL in the user's browser when a deployment completes. The URL for logs and deployments is printed to the console and the user can choose to open it if needed.

## 0.15.5

### Patch Changes

- 823e7ab: feat(cli): add `gram redeploy` command to clone and redeploy existing deployments

  fix(dashboard): show redeploy button on every deployment detail page and add visible Deployments navigation to sources page

## 0.15.4

### Patch Changes

- 484bbe0: Enable renaming of MCP authorization headers and with user friendly display names. These names are used as the default names of environment variables on the user facing MCP config.

## 0.15.3

### Patch Changes

- 8cf2f54: export stage CLI as go library
- e715308: Refactor cli commands as exported go libraries.

## 0.15.2

### Patch Changes

- 98be8a0: CLI opens deployment URL in browser after `gram push`

## 0.15.1

### Patch Changes

- 45bea6e: Pin to older mcp-remote@0.1.25 to avoid classic claude desktop issue with selecting the oldest node version on the machine. Versions pre v20 such as commonly available v18 make it not possible for people to load an mcp

## 0.15.0

### Minor Changes

- 1c836a2: Proxy remote file uploads through gram server

## 0.14.0

### Minor Changes

- 809fb43: `gram update` command to provide a self-update mechanism for the Gram CLI.
  `--check` flag - Check for updates without installing (dry-run)
  `--force` flag - Force update even if already on latest version

## 0.13.5

### Patch Changes

- a5a73fb: fix: correct `.mcpb` zip format for Claude Desktop and Cursor deep link encoding

## 0.13.4

### Patch Changes

- 2cc9008: Update functions cli to better track long deployments.

## 0.13.3

### Patch Changes

- a52cc7d: fix: improve `gram install` for claude-desktop UX

## 0.13.2

### Patch Changes

- 3552ff0: modifies gram auth so it respects current project context on the initial auth and sets that as defaultProjectSlug

## 0.13.1

### Patch Changes

- b8ed917: feat: add --scope flag to gram install command to determine whether the mcp config is added to the user, project or local config locations.

## 0.13.0

### Minor Changes

- 31e555b: feat: Add gram install command for MCP server configuration & support common clients

  **Automatic Configuration**

  ```bash
  gram install claude-code --toolset speakeasy-admin
  ```

  - Fetches toolset metadata from Gram API
  - Automatically derives MCP URL from organization, project & environment or custom MCP slug
  - Intelligently determines authentication headers and environment variables from toolset security config
  - Uses toolset name as the MCP server name

  **Manual Configuration**

  ```bash
  gram install claude-code
  --mcp-url https://mcp.getgram.ai/org/project/environment
  --api-key your-api-key
  --header-name Custom-Auth-Header
  --env-var MY_API_KEY
  ```

  - Supports custom MCP URLs for non-Gram servers
  - Configurable authentication headers
  - Environment variable substitution for secure API key storage
  - Automatic detection of locally set environment variables (uses actual value if available)

## 0.12.0

### Minor Changes

- 0e8fb8f: feat: sign CLI binary with cosign to allow distribution for `aqua` and `mise`

## 0.11.6

### Patch Changes

- bee7eae: Updated error wrapping messages in the CLI API client to avoid redundant phrases
  when printed to user.
- 87151f0: fix: cli deployment logs incorrectly mapping to localhost

## 0.11.5

### Patch Changes

- 1275e02: Attempt to mitigate race condition in CLI release process in GitHub Actions.

## 0.11.4

### Patch Changes

- bab05ce: Adds support to the Playground for any tool type, notably enabling function tools to be used there

## 0.11.3

### Patch Changes

- f824633: Fixed an issue where Go's http.Client used by CLI was stripping the
  `Content-Length` header. This happens when Go cannot determine the content
  length from a given `io.Reader`. It will prefer to drop any custom
  `Content-Length` header in favor of using chunked transfer encoding. However
  this won't work when hitting Gram's assets API which expects an explicit
  `Content-Length` header to be on the request.
- dbf6700: When adding duplicate sources via `gram stage`, the last occurrence of
  each source slug is now retained, ensuring predictable behavior without
  erroring out.

## 0.11.2

### Patch Changes

- 6a816ad: Add a more inviting page for successful authentication

## 0.11.1

### Patch Changes

- 54b14bb: fixed GitHub release name

## 0.11.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

## 0.10.0

### Minor Changes

- 9fbd193: Introducing two new commands to the Gram CLI:

  ```
  gram stage openapi --slug <slug> --location <path>
  gram stage function --slug <slug> --location <path>
  ```

  These commands can be used to gradually build out deployment configs by
  adding OpenAPI documents and Gram Functions zip files as sources. After
  all sources are added, `gram push` can be used to deploy the staged
  configuration.

  In practice, this should make it easier to script a Gram deployment in CI/CD and
  locally compared to authoring a full deployment JSON config manually.

- 30f385c: Added a `--method replace|merge` flag to the `gram push` command. This flag
  allows users to specify whether a push should replace all previous deployment
  artifacts or merge on top of them. The default behavior is `--method merge`. As
  an illustrative example:

  **With `--method replace`:**

  ```
  T0:
    Current project artifacts:
      - petstore.openapi.yaml
      - greet.zip

  T1:
    User runs:
      gram stage function --slug ecommerce --location ecommerce.zip
      gram push --method replace

  T2:
    Resulting project artifacts:
      - ecommerce (ecommerce.zip)
  ```

  **With `--method merge` (the new default behavior):**

  ```
  T0:
    Current project artifacts:
      - petstore (petstore.openapi.yaml)
      - greeter (greet.zip)

  T1:
    User runs:
      gram stage function --slug ecommerce --location ecommerce.zip
      gram push --method merge

  T2:
    Resulting project artifacts:
      - petstore (petstore.openapi.yaml)
      - greeter (greet.zip)
      - ecommerce (ecommerce.zip)
  ```

### Patch Changes

- 789b304: Updated the deployment workflow in the CLI to not require a previous deployment
  ID when evolving.

## 0.9.0

### Minor Changes

- 6ac98df: Add whoami command to easily view details about the current profile specified in $HOME/.gram/profile.json
- 1470223: Support automated authentication for any user profile via `gram auth`

## 0.8.0

### Minor Changes

- fde5a08: Support function uploads
- c173592: Add profile support to CLI for storing and managing credentials. Users can now save their authentication credentials in named profiles, eliminating the need to pass them as explicit environment variables for each command invocation.

## 0.4.0

### Minor Changes

- d6923b6: Enable asset upload to gram via `gram upload`

### Patch Changes

- 38e7b8f: Release CLI with properly prefixed tags.
- 40f0565: Increase client timeout to 10 minutes

## 0.3.0

### Minor Changes

- fa60d03: Support YAML and TOML deployment configs
- e29c090: Implement status command

### Patch Changes

- 9d23ef1: Initial changelog entry for Gram CLI
