# server

## 0.9.7

### Patch Changes

- bab05ce: Adds support to the Playground for any tool type, notably enabling function tools to be used there
- 7afda6e: Allows the MCP metadata map to accept arbitrary value types as supported by the server

## 0.9.6

### Patch Changes

- 69e766a: Adds a page for viewing tool call logs from ClickHouse with a searchable table interface displaying tool call history and infinite scroll pagination with cursor-based navigation for efficient data loading.

## 0.9.5

### Patch Changes

- 7334ac8: fix the mcp server passthrough in gram functions. We receive the result content and respond with that

## 0.9.4

### Patch Changes

- 5b8a324: Supports returning meta tags in list tools and list resources. Supports a specific gram.ai/kind meta tag that tells us to treat the underlying function as an MCP server and a direct passthrough

## 0.9.3

### Patch Changes

- 4ae6852: Adds an icon to the mcpb installation method that will render in Claude Desktop alongside your tool calls
- 5038166: Introduced the ability to register \_meta tags for tools and resources

## 0.9.2

### Patch Changes

- 3c00725: Set of improvements for functions onboarding UX, including better support for mixed OpenAPI / Functions projects
- 99ef7d6: reinstroduced oauth protected resource, the way we are exposing this is generally correct even though many clients don't really process it yet
- 1a46e29: Allows MCP to work in browser based MCP inspector which was the original intention
- 6a2eecf: Sets up the ability to track gram functions memory and cpu usage per tool call coming from the function runner
- 12fef9e: Prevent nil pointer dereference panic during server and worker shutdown. This
  was happening because the Gram Functions orchestrator was retuning nil shutdown
  functions at various code paths.

## 0.9.1

### Patch Changes

- d6f5579: Adds a basic toolset UX for managing resources in the system adding/subtracting them per toolset
- 44cfc3b: Pass the appropriate uintptr value in the slog Record when logging in `oops.ShareableError.Log()`. Previously, all log messages had their source location being the Log method itself which was not helpful.
- 2fb24e6: Adds UI hints for custom tools, indicating which "subtools" are missing (if any), or just surfacing the list of subtools otherwise. Begins tracking the required subtools more powerfully in order to support Gram Functions.

## 0.9.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

### Patch Changes

- 671cc0e: Fixes two issues: 1) Producer scoped keys were incorrectly not able to access MCP servers, the app documents them as a superset on consumer and we had a bug. 2) The MCP install page was incorrectly forming a URL without the MCP Slug.
- 4680971: Implements listing resources into our actual MCP Server layer. Also implements the gateway proxy for resources currently only being served from functions. Billing/Metrics wise we still treat fetching a resources as a tool call, but there are resource attributes added onto this that would allow us to separate in the future.

## 0.8.1

### Patch Changes

- f3cea34: The first major wave of work for supporting MCP resources through functions includes creating the function_resource_definitions data model with corresponding indexes and resource_urns columns in toolset versions. It also introduces the function manifest schema for resources and implements deployment processing for function resources. A new resource URN type is added, which parses uniqueness from the URI as the primary key for resources in MCP. Additionally, this work enables adding and returning resources throughout the toolsets data model, preserves resources within toolset versions, and updates current toolset caching to account for them.

## 0.8.0

### Minor Changes

- f3ffd00: Preserve redirect URLs during log-in for unauthenticated browsers.

### Patch Changes

- 6c5d329: Remove errant authorization from image serving
- ac5cb3d: Add correct resolution of custom domains for private MCP servers in install pages

## 0.7.2

### Patch Changes

- 0fa05ce: Fix custom install page logos on custom domains
- 660c110: Support variations on any tool type. Allows the names of Custom Tools to now be edited along with all fields of Functions.
- 9f7f5ea: Correctly use the custom domain on install pages
- cb7fc5a: Update the gateway to check the `Gram-Invoke-ID` response header from Gram Functions tool calls before proxying the response back to the client. This is an added security measure that asserts the server that ran a function had access to the auth secret and was able to decrypt the bearer token successfully.

## 0.7.1

### Patch Changes

- 3ea6da7: feat: treat producer keys as a superset of consumer
- 8890c9e: Remove references to the `deleted` column for deployments_functions.
- d2283dd: Pass through only relevant environment variables to a given Gram Functions tool, as specified in the manifest, when invoking it.

## 0.7.0

### Minor Changes

- 9df917a: Adds the ability for users of private servers to load the install page for easy user install of MCPs.

### Patch Changes

- 3fa88db: Allow PCRE regex on incoming JSON sources, despite not necessarily being supported by Go's native regexp parsing.
- f15d1fe: Implements the boilerplate of being able to parse openIdConnect securitySchemes and treat the accessToken produced as a possible implementation of MCP OAuth
- 9df917a: fix: update to use mcpb instead of dxt nomenclature for MCP installation pages

## 0.6.0

### Minor Changes

- 806beca: Introducing support for Gram Functions as part of deployments. As part of deployment processing, each function attached to a deployment will have a Fly.io app created for it which will eventually receive tool calls from the Gram server.

  ## What are Gram Functions?

  Gram Functions are serverless functions that are exposed as LLM tools to be used in your toolsets and MCP servers. They can execute any arbitrary code and make the result available to LLMs. This allows you to go far beyond what is possible with today's OpenAPI artifacts alone

  At its code, a Gram Function is zip file containing at least two files: `manifest.json` and `functions.ts`.

  ### `manifest.json`

  This is a JSON file describing the tools including their names, descriptions, input schemas and any environment variables they require. For example:

  ```json
  {
    "version": "0.0.0",
    "tools": [
      {
        "name": "add",
        "description": "Add two numbers",
        "inputSchema": {
          "type": "object",
          "properties": {
            "a": { "type": "number" },
            "b": { "type": "number" }
          },
          "required": ["a", "b"]
        }
      },
      {
        "name": "square_root",
        "description": "Calculate the square root of a number",
        "inputSchema": {
          "type": "object",
          "properties": {
            "a": { "type": "number" }
          },
          "required": ["a"]
        }
      }
    ]
  }
  ```

  ### `functions.js` / `functions.ts`

  A JavaScript or TypeScript file exporting the actual function implementation for tool calls. Here's a function that implements the manifest above:

  ```javascript
  function json(value: unknown) {
    return new Response(JSON.stringify(value), {
      headers: { "Content-Type": "application/json" },
    });
  }

  export async function handleToolCall({ name, input }) {
    // process.env will also containe any environment variables passed on from
    // Gram.

    switch (name) {
      case "add":
        return json({ value: input.a + input.b });
      case "square_root":
        return json({ value: Math.sqrt(input.a) });
      default:
        throw new Error(`Unknown tool: ${name}`);
    }
  }
  ```

  Notably:
  - The file must export an async function called `handleToolCall` which takes the tool name and input object as parameters.
  - This function must return a `Response` object.
  - You can use any npm packages you like but you must ensure they are included in the zip file.

  ## What is currently supported?
  - We currently only support TypeScript/JavaScript functions and deploy them into small Firecracker microVMs running Node.js v22.
  - Each function zip file must be a little under 750KiB in size or less than 1MiB when encoded in base64.
  - Third-party dependencies are supported but you must decide how to include in zip archives. You may bundle everything into a single file or include a `package.json` and node_modules directory in the zip file. As long as the total size is under the limit, it should work.
  - The code will be deployed into `/var/task` in the microVM.
  - The code will only have permission to write to `/tmp`.
  - The code must not depend on data persisting to disk between successive tool calls.

- 104896e: Support tool calling to Gram Functions. This now means that you can deploy
  javascript/typescript code to Gram and expose it as tools in your MCP servers.
  This code runs in a secure sandbox on fly.io and allows you to run arbitrary
  that performs all sorts of tasks.

### Patch Changes

- c88b97f: Trim slugs to comply with 128-character limits.
- d8bd8c1: Restore security for HTTP tools in the MCP tool calling handler
- 143d76e: A database migration to support Gram Functions is added which includes:
  - A new table called `fly_apps` to store details about provisioned fly.io apps.
  - Columns in both `projects` and `deployments_functions` tables that allow pinning to a specific version of the Gram Functions runner.

## 0.5.0

### Minor Changes

- 31d661e: Add cache in front of describe toolset

### Patch Changes

- 2905669: Improve fallbacks when reading period usage. Fixes a minor race condition when a customer has only just subscribed
- 36d7a3a: Properly set schema $defs when extracting tool schemas. Resolves an issue where recursive schemas were being created invalid.
- e768e4d: Introduce “healing” of invalid tool call arguments. For certain large tool input JSON schemas, LLMs can sometimes pass in stringified JSON where literal JSON is expected. We can unpack the correct json object out of this, even after the LLM mistake.

  **Before healing**

  ```json
  {
    "name": "get_weather",
    "input": "{\"lat\": 123, \"lng\": 456}"
  }
  ```

  **After healing**

  ```json
  {
    "name": "get_weather",
    "input": { "lat": 123, "lng": 456 }
  }
  ```

- a3b4abe: feat: propogate through function environment variables on toolset

## 0.4.0

### Minor Changes

- 276d265: Support API key validation (/rpc/keys.verify)
- 7912397: Add endpoint to expose a project's active deployment

### Patch Changes

- e76199f: fill default schema for prompt templates
- 004e017: fix: consistent environment overrides"
- 148c86f: install page reflects pure toolset name
- 85ceb4c: Add JSON schema validation to tool schema generation
- 6a331ac: feat: connection function tools to toolset concept
- 6f11e8e: add ability to configure install pages and render configurations onto pages
- ae5a041: Add clickhouse dependency
- 094c3ee: Extract tools concurrently from incoming specs.
- 5a32fd7: fix: ensure custom domain ingress has proper regex annotation
- 41b5a22: feat: add consistent trace id to tool call requests
- 4fd085a: Update sanitization logic to properly coerce into the regex
- 8d7852e: add table for install page metadata
- 40ef4c9: feat: add project id to function tools model
- 663c572: omit access token which overrides intended oauth behavior
- 36454a3: patch nil dereference
- c40d9c0: fix: adjust cors policy for mcp oauth routes
- 180bfca: restore old location for install page (no /install)
- dcd0055: feat: billing usage tracking federation

## 0.3.0

### Minor Changes

- f17c187: Support uploading Gram Functions as part of deployments
- 9a93cdd: adds branding and improved install instructions to mcp install page

### Patch Changes

- b449904: Properly pass in user_config to dxt files
- b96cb53: Add functions_access table
- 155c2e1: Add gram cli v0.1.0
- bd15d15: Fixes mobile layout for install page
- e68386d: fix openrouter key refresh
- 4e0646e: Allow leading and trailing underscores and dashes in tool names and slugs
- ee7b023: Add basic validation for deployment attachments
- 395b806: small fixes to mcp install page
- 49a5851: support non security scheme input header parameters
- a91a5eb: make billing stub no-op in local dev thus preserving desired state

## 0.2.0

### Minor Changes

- 6d8ee87: Add an improved MCP installation page that offers one-click install to several popular clients as well as a more aesthetically pleasing presentation
- c7864b6: Improved revision of the server install page with simpler ergonomics and more install options
- 87136d0: Rename deployment fields for asset/tool count to prefix with openapiv3 and make room for new tool types/sources.

### Patch Changes

- ceb108f: Fix flakes in global ordering unit test.
- ece9cbb: ensure the latest tools in the system reflect from the latest successful deployment
- db11042: Add tool type field to HTTP tool definitions
- 33cdfa7: Repairs errant release of install page by actually including assets
- bc7faae: fix scope oauth variables to security key
- f5dc8b5: Include org id in tracing spans for polar
- 61f419f: Add OpenTelemetry tracing around OpenAPI processing

## 0.1.5

### Patch Changes

- 635a012: Avoid a nil pointer dereference on API-based requests to create deployments.
- 94c0009: Clear tools from previous deployment attempts when retrying deployments
- c270b33: fix implement hardcoded limit for tool calls until polar max can be trusted
- 7b65af4: Fill in project id and openapi document id when creating http security records during deployment processing
- bb6393f: handle subscription downgrade in polar webhook
- 0158ef8: Fall back to free tier for orgs with canceled subscriptions
- f150c54: correct openrouter threshold for pro tier
- fbcbeee: start checking tool call usage in free tier

## 0.1.4

### Patch Changes

- ef1eff3: fix a bug updating account type from polar

## 0.1.3

### Patch Changes

- a160361: update openrouter playground credits on account upgrade/downgrade

## 0.1.2

### Patch Changes

- dd769ee: update proxy parsing to better handle large numbers in params

## 0.1.1

### Patch Changes

- acf6726: Expose the kind of prompt templates, and do not count higher order tools as prompts in the dashboard.

## 0.1.0

### Minor Changes

- d4dbddd: Manage versioning and changelog with [changesets](https://github.com/changesets/changesets)
