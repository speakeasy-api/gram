[**@gram-ai/elements v1.40.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / SectionMatch

# Interface: SectionMatch

One flagged finding within a tool section.

## Properties

### value

> **value**: `string`

Literal substring to highlight and step to.

***

### label?

> `optional` **label**: `string`

Short rule label shown when this match is active (e.g. "pii.phone_number").

***

### onExclude()?

> `optional` **onExclude**: () => `void`

Optional action for this finding, surfaced as a button while it is the
active match (e.g. open the create-exclusion flow).

#### Returns

`void`
