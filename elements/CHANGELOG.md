# @gram-ai/elements

## 1.26.0

### Minor Changes

- 9cb2f0e: Chart plugin and generative UI overhaul

  **Chart Plugin**
  - Replace Vega-Lite with Recharts for React 19 compatibility
  - Add themed tooltips using CSS variables (oklch colors)
  - Update chart stories to use MCP orders summary tool

  **Generative UI**
  - Add macOS-style window frames with traffic light buttons
  - Add whimsical cycling loading messages (50 messages, 2s fade transitions)
  - Streamline LLM prompt from ~150 lines to concise bulleted format

  **Component Fixes**
  - ActionButton executes tools via useToolExecution hook
  - Align Text, Badge, Progress props with LLM prompt specification
  - Fix catalog schema toolName → action mismatch
  - Fix setTimeout cleanup in CyclingLoadingMessage

  **Storybook**
  - Fix theme toggle causing full component remount

## 1.25.2

### Patch Changes

- e08b45e: Adds support for forwarding and storing user feedback. Incorporates the stored user feedback into chat resolution analysis

## 1.25.1

### Patch Changes

- 63bb328: Fix tool group count showing inflated numbers when loading chat history. The server accumulates all tool calls from a turn into each assistant message, causing duplicate tool-call parts when converting messages for the UI. Added deduplication in the message converter so each tool call only appears once. Also fixed `buildAssistantContentParts` silently dropping tool calls when assistant content is a string.

## 1.25.0

### Minor Changes

- feea712: Add `reactCompat()` Vite plugin for React 16/17 support. Users on older React versions can add one line to their Vite config to polyfill React 18 APIs (`useSyncExternalStore`, `useId`, `useInsertionEffect`, `startTransition`, `useTransition`, `useDeferredValue`) used by Elements and its dependencies.

## 1.24.2

### Patch Changes

- 46004f8: Fix tool mentions not working inside Shadow DOM. The composer's tool mention autocomplete used `document.querySelector` to find the textarea, which can't reach elements inside a shadow root. Changed to use `getRootNode()` so it correctly queries within the Shadow DOM when present.

## 1.24.1

### Patch Changes

- ca387c6: Fix dark mode text colors for approval and deny buttons in tool approval UI
- 6793e29: Fix thread list and tool approval UI for small containers and dark mode:
  - Fix scroll-to-bottom arrow invisible in dark mode
  - Make tool approval Deny/Approve buttons responsive with container queries
  - Fix popover toggle race condition using composedPath() for Shadow DOM support
  - Fix popover and tooltip z-index ordering
  - Fix thread list item title text wrapping
  - Resize welcome suggestions layout for small containers

## 1.24.0

### Minor Changes

- 08e4fb5: Add experimental message feedback UI with like/dislike buttons that appear after assistant messages. Enable with `thread.experimental_showFeedback: true` config option. Allows users to mark conversations as resolved.
- 2d520cb: Add support for follow-on suggestions within the Elements library
- 51b9f17: Add replay mode and cassette recording for Elements. The `<Replay>` component plays back pre-recorded conversations with streaming animations — no auth, MCP, or network calls required. The `useRecordCassette` hook and built-in composer recorder button (gated behind `VITE_ELEMENTS_ENABLE_CASSETTE_RECORDING` env var) allow capturing live conversations as cassette JSON files.

### Patch Changes

- c17b9f7: Fix logs page performance, responsive charts, tool output rendering, and streaming indicator
  - Memoize config objects and callbacks in Logs page and thread to prevent unnecessary re-renders
  - Fix tool group count using startIndex/endIndex instead of filtering all message parts
  - Fix shimmer CSS in shadow DOM by setting custom properties on .gram-elements
  - Auto-size charts to container width via ResizeObserver instead of fixed 400px minimum
  - Truncate large tool output to 50-line preview, skip shiki for content over 8K chars
  - Show pulsing dot indicator after tool calls while model is still running

- 438e1a7: Dropped the catalog dependency on react-query in favor of a direct dependency. This allows the elements package to be (p)npm linked into other local projects.

## 1.23.0

### Minor Changes

- 6744e5d: Add generative UI plugin for dynamic widget rendering. The plugin renders `ui` code blocks as interactive widgets including Card, Grid, Metric, Table, Badge, Progress, List, and ActionButton components. ActionButton enables triggering tool calls directly from generated UI.

## 1.22.5

### Patch Changes

- 258b503: Updated the message conversion logic to properly rehydrate assistant messages that include tool call results.

## 1.22.4

### Patch Changes

- a57b307: Fix resumption of chats
- 156bc66: Fix logs page on dashboard and correct display issues in Elements library

## 1.22.3

### Patch Changes

- d733319: Add chat-id header to mcp discovery

## 1.22.2

### Patch Changes

- 9073203: Fix elements onboarding in dashboard which was broken by shadow DOM changes

## 1.22.1

### Patch Changes

- 5c6f78a: Embed Elements chat in logs page

## 1.22.0

### Minor Changes

- adac3f8: Adds tool mentions / tagging to elements

## 1.21.3

### Patch Changes

- a0b7e13: feat: Add `gramEnvironment` config option to specify which environment's secrets to use for tool execution. When set, sends the `Gram-Environment` header to both MCP and completion requests.
- 43500b3: Add Shadow DOM style isolation for exported Elements components.

## 1.21.2

### Patch Changes

- 0472997: Disable retrying of messages until backend endpoint supports message branching

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
