[**@gram-ai/elements v1.16.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / ModalConfig

# Interface: ModalConfig

## Extends

- `ExpandableConfig`

## Properties

### expandable?

> `optional` **expandable**: `boolean`

Whether the modal or sidecar can be expanded

#### Inherited from

`ExpandableConfig.expandable`

***

### defaultExpanded?

> `optional` **defaultExpanded**: `boolean`

Whether the modal or sidecar should be expanded by default.

#### Default

```ts
false
```

#### Inherited from

`ExpandableConfig.defaultExpanded`

***

### dimensions?

> `optional` **dimensions**: [`Dimensions`](Dimensions.md)

The dimensions for the modal or sidecar window.

#### Inherited from

`ExpandableConfig.dimensions`

***

### defaultOpen?

> `optional` **defaultOpen**: `boolean`

Whether to open the modal window by default.

***

### title?

> `optional` **title**: `string`

The title displayed in the modal header.

#### Default

```ts
'Chat'
```

***

### position?

> `optional` **position**: [`ModalTriggerPosition`](../type-aliases/ModalTriggerPosition.md)

The position of the modal trigger

#### Default

```ts
'bottom-right'
```

***

### icon()?

> `optional` **icon**: (`state`) => `ReactNode`

The icon to use for the modal window.
Receives the current state of the modal window.

#### Parameters

##### state

`"open"` | `"closed"` | `undefined`

#### Returns

`ReactNode`
