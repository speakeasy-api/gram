# @gram-ai/elements

## 1.21.1

### Patch Changes

- ed50d35: Automatically sync peer dependencies into the rollupOptions.externals list

## 1.21.0

### Minor Changes

- 03f7cbe: Support passing a function for toolsRequiringApproval within the Elements human-in-the-loop configuration object
- 8b20bcf: Scopes all Elements tailwindcss to a root selector (.gram-elements) which wraps the component tree. The aim of this is to prevent Elements CSS polluting the application's CSS in which it is embedded
- 3be7ac7: Add error tracking and session replay capture to Elements library

### Patch Changes

- 5d14e1a: Add chromatic to Elements library to track visual regressions

## 1.20.2

### Patch Changes

- adc02ce: Adds error boundary to the Elements library

## 1.20.1

### Patch Changes

- 7506a42: Update elements react pinned version to match Gram
- b3ac308: Allow className to be passed to Chat component and update stories

## 1.20.0

### Minor Changes

- 950419c: Adds chat threads and history to elements

### Patch Changes

- 45eb983: Some charting improvements, both to style and reliability. Expands the plugin prompt, hopefully helping more models produce valid Vega

## 1.19.1

### Patch Changes

- f2fa135: Fix bin script

## 1.19.0

### Minor Changes

- 748c52e: No longer externalize assistant-ui

### Patch Changes

- 856576b: Fixes elements changelog due to prior failures to update
- a1231be: Adds install script to elements package

## 1.18.8

### Patch Changes

- f744f2b: Fix api typings

## 1.18.7

### Patch Changes

- 24f5bfa: Unblock reasoning models in elements

## 1.18.6

### Patch Changes

- 78415909: Update sidecar variant to include title & loader
- 2c393621: Include Elements src and srcmaps in the npm bundle for Elements to aid debugability

## 1.18.5

### Patch Changes

- 59f05eb8: Fixes fallback logic for api url

## 1.18.4

### Patch Changes

- da8896c4: Fix session fetching logic in elements hooks
- d6d7ebc6: Fix storybook setup

## 1.18.3

### Patch Changes

- 60555029: Fallback to modal variant if no variant provided in the config object

## 1.18.2

### Patch Changes

- f0dad26b: Adds support for UNSAFE_apiKey in Elements. This will be used during onboarding to allow users to quickly trial elements without needing to set up the sessions endpoint in their backend
- a8c46ef4: Fix release workflow

## 1.18.1

### Patch Changes

- 6c293607: Fix storybook stories to use local mcp server toolset prefix & split up into separate files for readability
- 6b98a0bc: Change elements to eagerly load (do not wait for MCP tools discovery)
- 75d14073: Moved away from a hardcoded Gram API URL. It is now possible to configure the API URL that Gram Elements uses to communicate with Gram using Vite's `define` setting or with `ElementsConfig["apiURL"]`.

## 1.18.0

### Minor Changes

- be238681: Support human-in-the-loop approval flow for a given set of tools.

### Patch Changes

- 337ccdbe: Refactored the Elements codebase to remove hard-coded references to Gram projects and MCP servers. Some of this hard-coding affected Storybook but there were instances where the session manager was pinned to a project called `default` that was also resolved.
- 657fc6a6: Fixes human-in-the-loop support for frontend-defined tools which have a different execution apparatus to MCP sourced tools

## 1.17.0

### Minor Changes

- c5242aa2: Support external model providers

### Patch Changes

- 4dccbf5c: Re-add prettier tailwind plugin to Elements
- dcb71c41: Update readme to reflect session changes

## 1.16.5

### Patch Changes

- 3d48d55: The chat handler has been removed as the chat request now happens client side. A new session handler has been added to the server package, which should be implemented by consumers in their backends.
- d19cb20: Fixes syncronization issue with chart plugin JSON parsing whilst streaming

## 1.16.4

### Patch Changes

- b44d018: Fix small display issues with modal variant

## 1.16.3

### Patch Changes

- 45035de: Fix tsdoc comments for several types within Elements library

## 1.16.2

### Patch Changes

- 990cc9e: Fix typedoc generation

## 1.16.1

### Patch Changes

- fc327c8: fixes release workflow

## 1.16.0

### Minor Changes

- eb72619: Gram Elements is a library of UI primitives for building chat-like experiences for MCP Servers.

  The first release of Gram Elements includes:
  - An all-in-one `<Chat />` component that encapsulates the entire chat lifecycle, including built-in support for tool calling and streaming responses.
  - A powerful configuration framework to refine the chat experience, including different layouts, theming, and much more.

### Patch Changes

- 6564e60: Fix publishing
