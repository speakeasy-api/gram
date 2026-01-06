[**@gram-ai/elements v1.16.5**](../README.md)

***

[@gram-ai/elements](../globals.md) / ElementsProviderProps

# Interface: ElementsProviderProps

## Properties

### children

> **children**: `ReactNode`

The children to render.

***

### config

> **config**: [`ElementsConfig`](ElementsConfig.md)

Configuration object for the Elements library.

***

### getSession?

> `optional` **getSession**: `GetSessionFn`

Function to retrieve the session token from the backend endpoint.

#### Example

```ts
const config: ElementsConfig = {
  getSession: async () => {
    return fetch('/chat/session').then(res => res.json()).then(data => data.client_token)
  },
}
```

#### Default

```ts
Use this default if you are using the Elements server handlers, and have mounted the session handler at /chat/session.
```
