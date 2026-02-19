[**@gram-ai/elements v1.27.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / CalendarProps

# Interface: CalendarProps

## Properties

### selected?

> `optional` **selected**: `object`

Selected date range

#### start

> **start**: `Date` \| `null`

#### end

> **end**: `Date` \| `null`

***

### onSelect()?

> `optional` **onSelect**: (`range`) => `void`

Called when a date or range is selected

#### Parameters

##### range

###### start

`Date`

###### end

`Date` \| `null`

#### Returns

`void`

***

### mode?

> `optional` **mode**: `"range"` \| `"single"`

Whether range selection is enabled

***

### minDate?

> `optional` **minDate**: `Date`

Disable dates before this

***

### maxDate?

> `optional` **maxDate**: `Date`

Disable dates after this

***

### className?

> `optional` **className**: `string`

Additional className
