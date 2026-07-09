[**@gram-ai/elements v1.41.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / SectionHighlight

# Interface: SectionHighlight

## Properties

### matches

> **matches**: [`SectionMatch`](SectionMatch.md)[]

Findings to highlight and step through with the next/prev controls.

***

### masked?

> `optional` **masked?**: `boolean`

Dot out the matched characters until the viewer reveals them (secrets).

***

### headerBadge?

> `optional` **headerBadge?**: `ReactNode`

Optional host-supplied badge rendered in the section header (e.g. a risk
pill). Replaces the default warning icon when present.

***

### tone?

> `optional` **tone?**: `"search"` \| `"risk"`

Mark colour: "risk" (red, default) for findings, "search" (yellow) for a
text-search hit.

***

### activeOccurrence?

> `optional` **activeOccurrence?**: `number` \| `null`

Search tone only: index of the active query occurrence within THIS section
(the unified thread navigator's current target). The host owns occurrence
stepping, so this is controlled: the occurrence at this index renders bright
and scrolls into view; null/undefined means this section holds no active
occurrence, so all its hits render pale.
