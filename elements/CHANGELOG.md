# @gram-ai/elements

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
