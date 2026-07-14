[**@gram-ai/elements v1.42.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ResolvedLink

# Interface: ResolvedLink

The outcome of resolving a markdown link `href`. Returned by a
[LinkResolver](../type-aliases/LinkResolver.md) so the host app can rewrite where (and how) an
assistant-authored link points.

## Properties

### href

> **href**: `string` \| `null`

The href to render. `null` signals "this is an internal reference the host
recognises but cannot resolve right now" — the anchor is dropped and its
text rendered inline, rather than producing a dead link (e.g. a partial
`href` mid-stream, or an unknown entity id).

***

### target?

> `optional` **target?**: `string`

Anchor `target`, e.g. `"_blank"` to open in a new tab.

***

### rel?

> `optional` **rel?**: `string`

Anchor `rel`. Defaults to `"noopener noreferrer"` when target is `_blank`.
