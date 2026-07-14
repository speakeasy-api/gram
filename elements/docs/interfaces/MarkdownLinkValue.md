[**@gram-ai/elements v1.42.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / MarkdownLinkValue

# Interface: MarkdownLinkValue

Host-supplied hooks for rendering links inside assistant markdown.

- `resolveLink` decides *where* a link points (e.g. rewriting an inline
  entity reference to a real route into the host app).
- `LinkComponent` decides *how* a link renders (e.g. the host's design-system
  link). Elements falls back to a plain `<a>` when none is supplied, so
  standalone usage keeps working.

## Properties

### resolveLink?

> `optional` **resolveLink?**: [`LinkResolver`](../type-aliases/LinkResolver.md)

***

### LinkComponent?

> `optional` **LinkComponent?**: [`MarkdownLinkComponent`](../type-aliases/MarkdownLinkComponent.md)
