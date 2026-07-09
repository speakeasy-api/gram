[**@gram-ai/elements v1.41.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / ToolUIProps

# Interface: ToolUIProps

## Properties

### name

> **name**: `string`

Display name of the tool

***

### icon?

> `optional` **icon?**: `ReactNode`

Optional icon to display (defaults to first letter of name)

***

### provider?

> `optional` **provider?**: `string`

Provider/source name (e.g., "Notion", "GitHub")

***

### status?

> `optional` **status?**: [`ToolStatus`](../type-aliases/ToolStatus.md)

Current status of the tool execution

***

### request?

> `optional` **request?**: `string` \| `Record`\<`string`, `unknown`\>

Request/input data - can be string or object

***

### result?

> `optional` **result?**: `string` \| `Record`\<`string`, `unknown`\> \| \{ `content`: [`ContentItem`](../type-aliases/ContentItem.md)[]; \}

Result/output data - can be string, object, or structured content array

***

### defaultExpanded?

> `optional` **defaultExpanded?**: `boolean`

Whether the tool card starts expanded

***

### requestHighlight?

> `optional` **requestHighlight?**: [`SectionHighlight`](SectionHighlight.md)

Flag matches inside the arguments (risk review).

***

### resultHighlight?

> `optional` **resultHighlight?**: [`SectionHighlight`](SectionHighlight.md)

Flag matches inside the output (risk review).

***

### nameQuery?

> `optional` **nameQuery?**: `string`

When set, highlight occurrences of this query (case-insensitive) in the
tool name — e.g. a thread search for "customer" lights up `get_customer`.

***

### nameActiveOccurrence?

> `optional` **nameActiveOccurrence?**: `number` \| `null`

Index of the active query occurrence within the tool name (the unified
navigator's current target), or null when the active occurrence isn't in the
name. Per-section args/output active occurrences ride their `*Highlight`.

***

### className?

> `optional` **className?**: `string`

Additional class names

***

### annotations?

> `optional` **annotations?**: `ToolAnnotations`

MCP tool annotations

***

### onApproveOnce?

> `optional` **onApproveOnce?**: () => `void`

Approval callbacks

#### Returns

`void`

***

### onApproveForSession?

> `optional` **onApproveForSession?**: () => `void`

#### Returns

`void`

***

### onDeny?

> `optional` **onDeny?**: () => `void`

#### Returns

`void`
