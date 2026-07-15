[**@gram-ai/elements v1.42.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / MessageContentProps

# Interface: MessageContentProps

## Properties

### content

> **content**: `string`

Raw assistant message content (markdown text optionally containing
```chart and ```ui fenced code blocks).

***

### className?

> `optional` **className?**: `string`

Optional className applied to the root container.

***

### markdown?

> `optional` **markdown?**: `boolean`

Render plain-text segments as markdown (matching `<MarkdownText />`)
instead of preformatted text. Fenced `chart`/`ui` blocks still render as
widgets either way.

***

### resolveLink?

> `optional` **resolveLink?**: [`LinkResolver`](../type-aliases/LinkResolver.md)

Resolver that rewrites link hrefs (only applies when `markdown` is true).

***

### linkComponent?

> `optional` **linkComponent?**: [`MarkdownLinkComponent`](../type-aliases/MarkdownLinkComponent.md)

Host link component used to render links (only applies when `markdown`).
