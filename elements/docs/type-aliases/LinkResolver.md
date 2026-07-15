[**@gram-ai/elements v1.42.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / LinkResolver

# Type Alias: LinkResolver

> **LinkResolver** = (`href`) => [`ResolvedLink`](../interfaces/ResolvedLink.md) \| `null`

Maps a markdown link `href` to a [ResolvedLink](../interfaces/ResolvedLink.md), or returns `null` to
leave the link untouched (rendered as a normal anchor).

Elements stays agnostic to any URL scheme: the host supplies this resolver
(via [ElementsConfig.resolveLink](../interfaces/ElementsConfig.md#resolvelink) for live chat, or the `resolveLink`
prop on `<Markdown>` / `<MessageContent>` for static viewers) and decides
which hrefs are internal entity references and what real route they map to.

## Parameters

### href

`string`

## Returns

[`ResolvedLink`](../interfaces/ResolvedLink.md) \| `null`
