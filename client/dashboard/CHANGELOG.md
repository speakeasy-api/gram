# @gram/dashboard

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
