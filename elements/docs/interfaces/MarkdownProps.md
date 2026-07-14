[**@gram-ai/elements v1.42.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / MarkdownProps

# Interface: MarkdownProps

## Properties

### children

> **children**: `string`

Raw markdown text.

***

### className?

> `optional` **className?**: `string`

Optional className applied to the `.aui-md` root wrapper.

***

### resolveLink?

> `optional` **resolveLink?**: [`LinkResolver`](../type-aliases/LinkResolver.md)

Optional resolver that rewrites link hrefs (e.g. turning inline entity
references into links into the host app). Static viewers render outside an
`ElementsProvider`, so they pass the link hooks here instead of via config.

***

### linkComponent?

> `optional` **linkComponent?**: [`MarkdownLinkComponent`](../type-aliases/MarkdownLinkComponent.md)

Optional `<a>`-shaped component used to render links with the host's design
system (e.g. a Moonshine `Link`). Falls back to a plain `<a>` when omitted.
