# @gram/client

## 0.27.0

### Minor Changes

- 514fce6: Improve observability chat logs with server-side sorting (sort_by/sort_order params), sticky pagination with page count, N/A score indicator with tooltip for unscored sessions, Shiki syntax highlighting for code blocks, character-based truncation with "Show more" button, System Prompt tab in chat detail panel, and Tool Result labeling for tool messages.

## 0.27.4

### Patch Changes

- f635e22: Support for [MCP tool annotations](https://modelcontextprotocol.io/legacy/concepts/tools#tool-annotations). Tool annotations provide additional metadata about a tool’s behavior,
  helping clients understand how to present and manage tools. These annotations are hints that describe the nature and impact of a tool, but should not be relied upon for security decisions.

  The MCP specification defines the following annotations for tools that Gram now supports for external mcp servers sourced from the Catalog as well as HTTP based tools.

  | Annotation        | Type    | Default | Description                                                                                                                          |
  | ----------------- | ------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------ |
  | `title`           | string  | -       | A human-readable title for the tool, useful for UI display                                                                           |
  | `readOnlyHint`    | boolean | false   | If true, indicates the tool does not modify its environment                                                                          |
  | `destructiveHint` | boolean | true    | If true, the tool may perform destructive updates (only meaningful when `readOnlyHint` is false)                                     |
  | `idempotentHint`  | boolean | false   | If true, calling the tool repeatedly with the same arguments has no additional effect (only meaningful when `readOnlyHint` is false) |
  | `openWorldHint`   | boolean | true    | If true, the tool may interact with an "open world" of external entities                                                             |

  Tool annotations can be edited in the playground or in the tools tab of a specific MCP server.

## 0.27.3

### Patch Changes

- b2347fc: Adds a new telemetry endpoint to fetch user usage data
- a34d18a: Adds chat resolution stats in telemetry metrics

## 0.27.1

### Patch Changes

- e08b45e: Adds support for forwarding and storing user feedback. Incorporates the stored user feedback into chat resolution analysis

## 0.26.18

### Patch Changes

- a7422f8: feat: add OAuth support for external MCP servers in the Playground
- a753172: feat: customize documentation button text on MCP install page
- 6e29702: Adds a new endpoint to get metrics per user. Allows filtering logs per user.
- 1f74200: Fixes issue with loading of metrics when logs are disabled.

## 0.26.13

### Patch Changes

- c9b74af: Adds a new endpoint to list chats grouped by ID

## 0.26.9

### Patch Changes

- 659d955: Add MCP JSON export API with API key authentication that allows customers to programmatically retrieve server information per MCP server
- afb9fbb: Adds new endpoint to retrieve summarized project metrics
- 90ad1ba: Add support for install page redirect URLs

## 0.27.0

### Minor Changes

- 834a770: Removes old tool toolmetrics logs logic and endpoints.

## 0.25.16

### Patch Changes

- 484bbe0: Enable renaming of MCP authorization headers and with user friendly display names. These names are used as the default names of environment variables on the user facing MCP config.

## 0.25.12

### Patch Changes

- 0fd8d39: Adds a new Gram endpoint to update a chat title

## 0.25.8

### Patch Changes

- d972d1b: Adds ability to filter telemetry logs by multiple Gram URNs
- 3a82c2e: Adds enabled field to telemetry API response indicating whether logging is enabled or not

## 0.24.2

### Patch Changes

- 7e5e7c8: Adds a new telemetry endpoint to the Gram API

## 0.24.0

### Minor Changes

- 8c865e1: Introduce the ability to browse entries from MCP-spec conformant registries from Gram Dashboard source import modal

### Patch Changes

- 811989e: Enable private MCP servers with Gram account authentication

  This change allows private MCP servers to require users to authenticate
  with their Gram account. When enabled, only users with access to the
  server's organization can utilize it.

  This is ideal for MCP servers that require sensitive credentials (such as API
  keys), as it allows organizations to:
  - Secure access to servers handling sensitive secrets (via Gram Environments)
  - Eliminate the need for individual users to configure credentials during installation
  - Centralize authentication and access control at the organization level

- 76beb93: Added support for ephemeral chat sessions, allowing secure invocation of chat completions from the browser

## 0.22.0

### Minor Changes

- 1c836a2: Proxy remote file uploads through gram server

## 0.21.6

### Patch Changes

- 949787b: update chat credit billing

## 0.21.2

### Patch Changes

- 4228c3e: Implements passthrough oauth support for function tools via oauthTarget indicator. Also simplifies the oauth proxy redirect for more recent usecases

## 0.20.1

### Patch Changes

- c2ea282: admin view for creating oauth proxies

## 0.19.0

### Minor Changes

- 6716410: Add the ability to attach gram environments at the toolset level for easier configuration set up

## 0.18.1

### Patch Changes

- cf3e81b: non blocking deployment creation

## 0.17.3

### Patch Changes

- 77446ee: fully connects server url tracking feature in opt in tool call logs

## 0.17.2

### Patch Changes

- bb37fed: creates the concept of user controllable product features, opens up logs to self-service enable/disable control

## 0.16.7

### Patch Changes

- 2db3a23: Add filtering support to the tool call logs table

## 0.16.3

### Patch Changes

- 7afda6e: Allows the MCP metadata map to accept arbitrary value types as supported by the server

## 0.15.3

### Patch Changes

- 5038166: Introduced the ability to register \_meta tags for tools and resources

## 0.14.17

### Patch Changes

- 3c00725: Set of improvements for functions onboarding UX, including better support for mixed OpenAPI / Functions projects

## 0.14.16

### Patch Changes

- d6f5579: Adds a basic toolset UX for managing resources in the system adding/subtracting them per toolset
- 2fb24e6: Adds UI hints for custom tools, indicating which "subtools" are missing (if any), or just surfacing the list of subtools otherwise. Begins tracking the required subtools more powerfully in order to support Gram Functions.

## 0.14.14

### Patch Changes

- f3cea34: The first major wave of work for supporting MCP resources through functions includes creating the function_resource_definitions data model with corresponding indexes and resource_urns columns in toolset versions. It also introduces the function manifest schema for resources and implements deployment processing for function resources. A new resource URN type is added, which parses uniqueness from the URI as the primary key for resources in MCP. Additionally, this work enables adding and returning resources throughout the toolsets data model, preserves resources within toolset versions, and updates current toolset caching to account for them.

## 0.14.11

### Patch Changes

- 660c110: Support variations on any tool type. Allows the names of Custom Tools to now be edited along with all fields of Functions.

## 0.14.7

### Patch Changes

- 8972d1d: feat: update client to account for function tool types"
