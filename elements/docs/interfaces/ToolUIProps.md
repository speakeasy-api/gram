[**@gram-ai/elements v1.37.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ToolUIProps

# Interface: ToolUIProps

## Properties

### name

> **name**: `string`

Display name of the tool

***

### icon?

> `optional` **icon**: `ReactNode`

Optional icon to display (defaults to first letter of name)

***

### provider?

> `optional` **provider**: `string`

Provider/source name (e.g., "Notion", "GitHub")

***

### status?

> `optional` **status**: [`ToolStatus`](../type-aliases/ToolStatus.md)

Current status of the tool execution

***

### request?

> `optional` **request**: `string` \| `Record`\<`string`, `unknown`\>

Request/input data - can be string or object

***

### result?

> `optional` **result**: `string` \| `Record`\<`string`, `unknown`\> \| \{ `content`: [`ContentItem`](../type-aliases/ContentItem.md)[]; \}

Result/output data - can be string, object, or structured content array

***

### defaultExpanded?

> `optional` **defaultExpanded**: `boolean`

Whether the tool card starts expanded

***

### requestHighlight?

> `optional` **requestHighlight**: [`SectionHighlight`](SectionHighlight.md)

Flag matches inside the arguments (risk review).

***

### resultHighlight?

> `optional` **resultHighlight**: [`SectionHighlight`](SectionHighlight.md)

Flag matches inside the output (risk review).

***

### className?

> `optional` **className**: `string`

Additional class names

***

### annotations?

> `optional` **annotations**: `ToolAnnotations`

MCP tool annotations

***

### onApproveOnce()?

> `optional` **onApproveOnce**: () => `void`

Approval callbacks

#### Returns

`void`

***

### onApproveForSession()?

> `optional` **onApproveForSession**: () => `void`

#### Returns

`void`

***

### onDeny()?

> `optional` **onDeny**: () => `void`

#### Returns

`void`
