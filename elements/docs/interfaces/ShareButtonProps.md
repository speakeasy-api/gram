[**@gram-ai/elements v1.26.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ShareButtonProps

# Interface: ShareButtonProps

## Properties

### onShare()?

> `optional` **onShare**: (`result`) => `void`

Called when the share action completes.
Receives the share URL on success, or an Error on failure.
Use this to show toast notifications or track analytics.

#### Parameters

##### result

\{ `url`: `string`; \} | \{ `error`: `Error`; \}

#### Returns

`void`

***

### buildShareUrl()?

> `optional` **buildShareUrl**: (`threadId`) => `string`

Custom URL builder. By default, appends `?threadId={id}` to current URL.
Return the full share URL.

#### Parameters

##### threadId

`string`

#### Returns

`string`

***

### variant?

> `optional` **variant**: `"default"` \| `"outline"` \| `"ghost"`

Button variant

#### Default

```ts
"ghost"
```

***

### size?

> `optional` **size**: `"default"` \| `"icon"` \| `"sm"` \| `"lg"`

Button size

#### Default

```ts
"sm"
```

***

### className?

> `optional` **className**: `string`

Additional CSS classes

***

### children?

> `optional` **children**: `ReactNode`

Custom button content. If not provided, shows icon + "Share chat"
