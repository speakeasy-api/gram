[**@gram-ai/elements v1.38.2**](../README.md)

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

> `optional` **className**: `string`

Optional className applied to the root container.

***

### markdown?

> `optional` **markdown**: `boolean`

Render plain-text segments as markdown (matching `<MarkdownText />`)
instead of preformatted text. Fenced `chart`/`ui` blocks still render as
widgets either way.
