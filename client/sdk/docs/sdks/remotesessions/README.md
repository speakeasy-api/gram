# RemoteSessions

## Overview

Operator visibility into remote_sessions Gram is holding on a principal's behalf. Read + revoke; sessions are written by /mcp/{slug}/remote_login_callback and the silent-refresh path. access_token_encrypted and refresh_token_encrypted are never returned.

### Available Operations

* [list](#list) - listRemoteSessions remoteSessions
* [revoke](#revoke) - revokeRemoteSession remoteSessions

## list

List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRemoteSessions" method="get" path="/rpc/remoteSessions.list" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessions.list();

  for await (const page of result) {
    console.log(page);
  }
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { remoteSessionsList } from "@gram/client/funcs/remoteSessionsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionsList(gram);
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
    console.log(page);
  }
  } else {
    console.log("remoteSessionsList failed:", res.error);
  }
}

run();
```

### React hooks and utilities

This method can be used in React components through the following hooks and
associated utilities.

> Check out [this guide][hook-guide] for information about each of the utilities
> below and how to get started using React hooks.

[hook-guide]: ../../../REACT_QUERY.md

```tsx
import {
  // Query hooks for fetching data.
  useRemoteSessions,
  useRemoteSessionsSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useRemoteSessionsInfinite,
  useRemoteSessionsInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRemoteSessions,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRemoteSessions,
  invalidateAllRemoteSessions,
} from "@gram/client/react-query/remoteSessionsList.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListRemoteSessionsRequest](../../models/operations/listremotesessionsrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListRemoteSessionsSecurity](../../models/operations/listremotesessionssecurity.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListRemoteSessionsResponse](../../models/operations/listremotesessionsresponse.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## revoke

Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="revokeRemoteSession" method="post" path="/rpc/remoteSessions.revoke" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.remoteSessions.revoke({
    id: "7f80e5ba-2e0f-49ea-acb3-d6666881397c",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { remoteSessionsRevoke } from "@gram/client/funcs/remoteSessionsRevoke.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionsRevoke(gram, {
    id: "7f80e5ba-2e0f-49ea-acb3-d6666881397c",
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("remoteSessionsRevoke failed:", res.error);
  }
}

run();
```

### React hooks and utilities

This method can be used in React components through the following hooks and
associated utilities.

> Check out [this guide][hook-guide] for information about each of the utilities
> below and how to get started using React hooks.

[hook-guide]: ../../../REACT_QUERY.md

```tsx
import {
  // Mutation hook for triggering the API call.
  useRevokeRemoteSessionMutation
} from "@gram/client/react-query/remoteSessionsRevoke.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.RevokeRemoteSessionRequest](../../models/operations/revokeremotesessionrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.RevokeRemoteSessionSecurity](../../models/operations/revokeremotesessionsecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |