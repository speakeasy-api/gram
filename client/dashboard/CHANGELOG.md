# @gram/dashboard

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
