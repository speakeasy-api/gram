# dashboard

## 0.26.3

### Patch Changes

- 12e825c: Add hide/show toggle for environment variable inputs

## 0.26.2

### Patch Changes

- 81be736: Updates dashboard to only use telemetry API
- Updated dependencies [f2fa135]
  - @gram-ai/elements@1.19.1

## 0.26.1

### Patch Changes

- Updated dependencies [856576b]
- Updated dependencies [a1231be]
- Updated dependencies [748c52e]
  - @gram-ai/elements@1.19.0

## 0.26.0

### Minor Changes

- eefebf6: Add updated elements onboarding

### Patch Changes

- Updated dependencies [f744f2b]
  - @gram-ai/elements@1.18.8

## 0.25.2

### Patch Changes

- f0dad26: Adds support for UNSAFE_apiKey in Elements. This will be used during onboarding to allow users to quickly trial elements without needing to set up the sessions endpoint in their backend

## 0.25.1

### Patch Changes

- 8ad0455: Ensure delete source dialog closes after completion
- 0583dc0: Improves logs side panel to make it wider and more human-readable
- Updated dependencies [d972d1b]
- Updated dependencies [3a82c2e]
  - @gram/client@0.25.8

## 0.25.0

### Minor Changes

- 01932db: Removes legacy logs page, replaced with a new page for improved user experience

### Patch Changes

- c8c45b5: add a source detail page for imported mcp servers

## 0.24.0

### Minor Changes

- 0341739: Add a new telemetry page to view logs grouped by tool calls

### Patch Changes

- b73b92d: Added empty state component for catalog search results

## 0.23.1

### Patch Changes

- Updated dependencies [7e5e7c8]
  - @gram/client@0.24.2

## 0.23.0

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

- 6e84b55: Allow external mcp sources to be renamed in the Gram UI
- Updated dependencies [811989e]
- Updated dependencies [76beb93]
- Updated dependencies [8c865e1]
  - @gram/client@0.24.0

## 0.22.3

### Patch Changes

- ba502dc: fix playground tools list now updates immediately when adding/removing tools from a toolset
- abbb9a3: Don't brick page when certain dialogs are closed. Also improves the mcp config dialog to not overflow the entire screen

## 0.22.2

### Patch Changes

- 45bea6e: Pin to older mcp-remote@0.1.25 to avoid classic claude desktop issue with selecting the oldest node version on the machine. Versions pre v20 such as commonly available v18 make it not possible for people to load an mcp

## 0.22.1

### Patch Changes

- a5d6df2: fix playground tool parameters not rendering on initial load and add horizontal scroll to responses
- 013d15d: Restore chat history loading in playground after v5 AI SDK upgrade
- 2667ecf: Fixed radix warning about Dialog.Content not having a Dialog.Title child.
- 90a3b7b: Allow instances.get to return mcp server representations of a toolset. Remove unneeded environment for instances get
- c8a0376: - fix SSE streaming response truncation due to chunk boundary misalignment
  - `addToolResult()` was called following tool execution, the AI SDK v5 wasn't automatically triggering a follow-up LLM request with the tool results. This is a known limitation with custom transports (vercel/ai#9178).
- 1a63676: Replace Shiki with Monaco Editor for viewing large inline specs
- e9988d8: Ensure stable QueryClient is used for lifetime of web app especially during
  development mode hot reloads.

## 0.22.0

### Minor Changes

- 1c836a2: Proxy remote file uploads through gram server
- c213781: Upgrade to AI SDK 5 and improve playground functionality
  - Upgraded to AI SDK 5 with new chat transport and message handling
  - Fixed keyboard shortcuts in playground chat input - Enter now properly submits messages (Shift+Enter for newlines)
  - Fixed TextArea component to properly accept and forward event handlers (onKeyDown, onCompositionStart, onCompositionEnd, onPaste)
  - Fixed AI SDK 5 compatibility by changing maxTokens to maxOutputTokens in CustomChatTransport
  - Fixed Button variant types in EditToolDialog (destructive-secondary, secondary)
  - Fixed Input component onChange handler to use value parameter directly
  - Fixed type mismatches between ToolsetEntry and Toolset in Playground component
  - Added missing Tool type import

### Patch Changes

- Updated dependencies [1c836a2]
  - @gram/client@0.22.0

## 0.21.1

### Patch Changes

- 59f21eb: fix: AddSourceDialog continue button not closing dialog when clicked
- 5f6d646: Allow uploading OpenAPI specs via remote url
- Updated dependencies [949787b]
  - @gram/client@0.21.6

## 0.21.0

### Minor Changes

- a041994: Introduces a new page for each source added to a users project. Source page provides details on the source, which toolsets its used and the abilty to attach an environment to a source.

### Patch Changes

- 4228c3e: Implements passthrough oauth support for function tools via oauthTarget indicator. Also simplifies the oauth proxy redirect for more recent usecases
- Updated dependencies [4228c3e]
  - @gram/client@0.21.2

## 0.20.1

### Patch Changes

- bc147e0: Updated dependencies to address dependabot security alerts
- c2ea282: admin view for creating oauth proxies
- Updated dependencies [c2ea282]
  - @gram/client@0.20.1

## 0.20.0

### Minor Changes

- 6716410: Add the ability to attach gram environments at the toolset level for easier configuration set up

### Patch Changes

- 6716410: restructure MCP authentication form to hide attach environments in advanced section
- e34b505: updating of openrouter key limits for chat based usage
- Updated dependencies [6716410]
  - @gram/client@0.19.0

## 0.19.5

### Patch Changes

- 6b04cc2: Updates playground chat models to a more modern list. Add Claude 4.5 Opus and ChatGPT 5.1

## 0.19.4

### Patch Changes

- 5396fd8: Update login page animation with interactive Gram function demo
  - Redesigned the login page animation from a sequential upload/generate flow to an interactive two-window demo
  - Replaced the generic Pet Store OpenAPI example with a real Gram function showcasing Supabase integration and UK property data querying
  - Added draggable, focusable windows to create a more engaging and realistic demonstration
  - Implemented progressive tool generation animation with reset functionality

## 0.19.3

### Patch Changes

- 8a92350: Fixes automatic closing behavior for Source Dialogs

## 0.19.2

### Patch Changes

- 44d4dca: Update dashboard to fix a few ui issues
- 0d4c7c8: Fix shiki theme in dark mode
- 3210d73: Add annoucement modal for Gram Functions
- 8bf8710: Introduces v2 of Dynamic Toolsets, combining learnings from Progressive and Semantic searches into one unified feature. Extremely token efficient, especially for medium and large toolsets.

## 0.19.1

### Patch Changes

- Updated dependencies [cf3e81b]
  - @gram/client@0.18.1

## 0.19.0

### Minor Changes

- c249bb0: Adds the ability to attach an environment to a source such that all tool calls originating from that source will have those environment variables apply

## 0.18.7

### Patch Changes

- 3552ff0: modifies gram auth so it respects current project context on the initial auth and sets that as defaultProjectSlug
- d9f4980: Fix onboarding steps to use `npm run` prefix

## 0.18.6

### Patch Changes

- 900d4cc: Adds the option to select/deselect all during tool management, for example when adding tools to a toolset
- 4b5a511: fix: logs page dialog content warning

## 0.18.5

### Patch Changes

- faef164: opens up logs to free tier
- 29aee79: fixes potentially duplicate env vars from functions in the UX and MCP config

## 0.18.4

### Patch Changes

- 10140df: Makes tool type filterable on more than just http tools (functions, custom)
- 77446ee: fully connects server url tracking feature in opt in tool call logs
- Updated dependencies [77446ee]
  - @gram/client@0.17.3

## 0.18.3

### Patch Changes

- ff7615f: Fixed a bug where the download link for function assets was incorrect on the Deployment page's Assets tab.
- bb37fed: creates the concept of user controllable product features, opens up logs to self-service enable/disable control
- Updated dependencies [bb37fed]
  - @gram/client@0.17.2

## 0.18.2

### Patch Changes

- 403a2c8: Fixes delete asset confirmation modal visual discrepancy and css fixes

## 0.18.1

### Patch Changes

- 9dd1b7a: Unify code block components

## 0.18.0

### Minor Changes

- 613f10e: Upgrade @speakeasy-api/moonshine to integrate bundle size reduction changes

## 0.17.8

### Patch Changes

- 192d6cb: temporarily clarify node version for functions
- 145295a: Changes default install method for Cursor MCPs to HTTP streaming
- 9963bbd: fix: multiple react versions in dev causes rules of hooks error

## 0.17.7

### Patch Changes

- f79fd52: Open dashboard from gram-build, better completing the flow starting from pnpm create

## 0.17.6

### Patch Changes

- 2db3a23: Add filtering support to the tool call logs table
- Updated dependencies [2db3a23]
  - @gram/client@0.16.7

## 0.17.5

### Patch Changes

- 8df9e59: Polish onboarding wizard with improved animations and code quality. Fixed memory leaks in WebGL particle effects, improved window trail particle density during fast movement, added scrollable content with blur gradients, and removed dead code.

## 0.17.4

### Patch Changes

- bab05ce: Adds support to the Playground for any tool type, notably enabling function tools to be used there
- Updated dependencies [7afda6e]
  - @gram/client@0.16.3

## 0.17.3

### Patch Changes

- 69e766a: Adds a page for viewing tool call logs from ClickHouse with a searchable table interface displaying tool call history and infinite scroll pagination with cursor-based navigation for efficient data loading.

## 0.17.2

### Patch Changes

- 4ae6852: Adds an icon to the mcpb installation method that will render in Claude Desktop alongside your tool calls
- Updated dependencies [5038166]
  - @gram/client@0.15.3

## 0.17.1

### Patch Changes

- 3c00725: Set of improvements for functions onboarding UX, including better support for mixed OpenAPI / Functions projects
- Updated dependencies [3c00725]
  - @gram/client@0.14.17

## 0.17.0

### Minor Changes

- aaad92f: Show Gram Functions on deployment pages

### Patch Changes

- 0b51c20: Add WebGL ASCII shader effects to onboarding wizard with interactive star particles
- d6f5579: Adds a basic toolset UX for managing resources in the system adding/subtracting them per toolset
- 321699e: Function-based tools can now be used in Custom Tools
- 2fb24e6: Adds UI hints for custom tools, indicating which "subtools" are missing (if any), or just surfacing the list of subtools otherwise. Begins tracking the required subtools more powerfully in order to support Gram Functions.
- Updated dependencies [d6f5579]
- Updated dependencies [2fb24e6]
  - @gram/client@0.14.16

## 0.16.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

### Patch Changes

- b6b4ed0: Better custom domain model ordering

## 0.15.1

### Patch Changes

- Updated dependencies [f3cea34]
  - @gram/client@0.14.14

## 0.15.0

### Minor Changes

- f3ffd00: Preserve redirect URLs during log-in for unauthenticated browsers.

### Patch Changes

- 73a7ffc: chore: Make tools dialog is wider, tool name prefixes are muted for easier legibility and mo tools found in search message has been improved for clarity

## 0.14.2

### Patch Changes

- 660c110: Support variations on any tool type. Allows the names of Custom Tools to now be edited along with all fields of Functions.
- Updated dependencies [660c110]
  - @gram/client@0.14.11

## 0.14.1

### Patch Changes

- b53cefb: Ensure all pages have proper bottom padding
- 64b8fc7: feat: Claude 4.5 Haiku available in playground model switcher

## 0.14.0

### Minor Changes

- 9df917a: Adds the ability for users of private servers to load the install page for easy user install of MCPs.

### Patch Changes

- f7a157d: Fix to set srcToolUrn when updating variations
- 9df917a: fix: update to use mcpb instead of dxt nomenclature for MCP installation pages

## 0.13.0

### Minor Changes

- 3cb955a: Dashboard support for the CLI authentication flow.

### Patch Changes

- 8148897: makes gram functions environments variables now account for in the MCP and gram environments UX
- 0f75503: adds a very basic few for displaying gram functions sources

## 0.12.0

### Minor Changes

- e956b16: feat: temperature slider in the playground
- fbdc9bd: feat: add @ symbol tool tagging syntax to playground
- 0e83d56: add new mcp configuration section for setting up install pages

### Patch Changes

- 90daf89: fix: prevent asset names from being cut off in deployments overview
- f312721: fix: only capture cmd-f in logs when logs section is focused
- Updated dependencies [8972d1d]
  - @gram/client@0.14.7

## 0.11.0

### Minor Changes

- 87136d0: Rename deployment fields for asset/tool count to prefix with openapiv3 and make room for new tool types/sources.

### Patch Changes

- 33cdfa7: Repairs errant release of install page by actually including assets
- 5a2214e: add GPT-5 to playground
- 0397ead: Enable cross-origin access to static assets

## 0.10.0

### Minor Changes

- 25b5d18: Migrate buttons from shadcn to design system component

## 0.9.3

### Patch Changes

- a1b3aaa: Revert to zod v3

## 0.9.2

### Patch Changes

- 72978ba: Standardize home page width
- acf6726: Expose the kind of prompt templates, and do not count higher order tools as prompts in the dashboard.

## 0.9.1

### Patch Changes

- d5e7b22: Avoid nil dereference in tool name callbacks used in ChatWindow

## 0.9.0

### Minor Changes

- d4dbddd: Manage versioning and changelog with [changesets](https://github.com/changesets/changesets)
