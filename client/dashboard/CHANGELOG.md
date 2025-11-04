# dashboard

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
