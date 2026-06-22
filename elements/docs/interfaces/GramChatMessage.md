[**@gram-ai/elements v1.37.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / GramChatMessage

# Interface: GramChatMessage

Represents a chat message from the Gram API. Only fields actually surfaced
through Elements' public converters are modelled; provider-specific extras
remain on the wire shape but are intentionally not part of the contract.

`tool_calls` is the JSON-encoded string the Gram chat service stores on
assistant rows; `tool_call_id` is the id the corresponding tool-response row
carries when `role === "tool"`.

## Properties

### id

> **id**: `string`

***

### seq?

> `optional` **seq**: `number`

***

### model

> **model**: `string`

***

### created\_at

> **created\_at**: `string` \| `Date`

***

### role

> **role**: `"system"` \| `"developer"` \| `"user"` \| `"assistant"` \| `"tool"`

***

### content?

> `optional` **content**: `GramChatContent` \| `null`

***

### name?

> `optional` **name**: `string`

***

### tool\_calls?

> `optional` **tool\_calls**: `string`

***

### tool\_call\_id?

> `optional` **tool\_call\_id**: `string`

***

### reasoning?

> `optional` **reasoning**: `string` \| `null`
