# cli

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
