[**@gram-ai/elements v1.22.0**](../README.md)

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

### showThreadList?

> `optional` **showThreadList**: `boolean`

Whether to show the thread list sidebar/panel.
Only applies when history is enabled.

#### Default

```ts
true when history.enabled is true
```
