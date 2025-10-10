# @gram/server

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
