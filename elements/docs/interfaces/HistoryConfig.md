[**@gram-ai/elements v1.33.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / HistoryConfig

# Interface: HistoryConfig

Configuration for chat history persistence.
When enabled, threads are persisted and can be restored from the thread list.

## Example

```ts
const config: ElementsConfig = {
  history: {
    enabled: true,
    showThreadList: true,
  },
}
```

## Properties

### enabled

> **enabled**: `boolean`

Whether to enable chat history persistence.
When true, threads will be saved and can be loaded from the thread list.

#### Default

```ts
false
```

***

### threadListFilters?

> `optional` **threadListFilters**: `Record`\<`string`, `string`\>

Extra query parameters forwarded to the thread-list request, used to
filter which conversations are shown. Opaque to Elements — the consumer
decides the keys (e.g. a search term, or a backend-specific scope). When
omitted, all of the caller's chats are listed.

***

### deferThreadIdMinting?

> `optional` **deferThreadIdMinting**: `boolean`

Let the backend own chat-id creation. When true, a brand-new thread does not
get a client-generated id; instead the transport assigns the id (e.g. one
the server minted on the first send, reported via the transport context's
`adoptChatId`). Use with a server-backed `transport`.

***

### showThreadList?

> `optional` **showThreadList**: `boolean`

Whether to show the thread list sidebar/panel.
Only applicable for widget and sidecar variants.
Only applies when history is enabled.

#### Default

```ts
true when history.enabled is true
```

***

### initialThreadId?

> `optional` **initialThreadId**: `string`

Initial thread ID to load when the component mounts.
When provided, Elements will automatically load and switch to this thread.
Useful for implementing chat sharing via URL parameters.

#### Example

```ts
// Read threadId from URL and pass to config
const searchParams = new URLSearchParams(window.location.search)
const threadId = searchParams.get('threadId')

<GramElementsProvider config={{
  history: {
    enabled: true,
    initialThreadId: threadId ?? undefined,
  },
}}>
```
