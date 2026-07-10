# OrganizationRemoteSessions

## Overview

Organization-administrator visibility into remote_sessions Gram is holding on a principal's behalf, across every project in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned.

### Available Operations

- [list](#list) - listClientSessions organizationRemoteSessions
- [refresh](#refresh) - refreshSession organizationRemoteSessions
- [revoke](#revoke) - revokeSession organizationRemoteSessions
- [revokeAll](#revokeall) - revokeAllClientSessions organizationRemoteSessions

## list

List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listOrganizationRemoteSessionClientSessions" method="get" path="/rpc/organizationRemoteSessions.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessions.list({
    clientId: "4104e40c-6015-460c-a3fc-320c2e5c6474",
  });

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
import { organizationRemoteSessionsList } from "@gram/client/funcs/organizationRemoteSessionsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionsList(gram, {
    clientId: "4104e40c-6015-460c-a3fc-320c2e5c6474",
  });
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
      console.log(page);
    }
  } else {
    console.log("organizationRemoteSessionsList failed:", res.error);
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
  useOrganizationRemoteSessionClientSessions,
  useOrganizationRemoteSessionClientSessionsSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useOrganizationRemoteSessionClientSessionsInfinite,
  useOrganizationRemoteSessionClientSessionsInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionClientSessions,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionClientSessions,
  invalidateAllOrganizationRemoteSessionClientSessions,
} from "@gram/client/react-query/organizationRemoteSessionsList.js";
```

### Parameters

| Parameter              | Type                                                                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListOrganizationRemoteSessionClientSessionsRequest](../../models/operations/listorganizationremotesessionclientsessionsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListOrganizationRemoteSessionClientSessionsSecurity](../../models/operations/listorganizationremotesessionclientsessionssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListOrganizationRemoteSessionClientSessionsResponse](../../models/operations/listorganizationremotesessionclientsessionsresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## refresh

Force an upstream token refresh on a single remote_session in the caller's organization, regardless of current access-token expiry. Returns the updated remote_session so callers can reflect the new expiry without a refetch. Fails with a bad-request error when the session holds no refresh token. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="refreshOrganizationRemoteSession" method="post" path="/rpc/organizationRemoteSessions.refresh" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessions.refresh({
    id: "debf903f-956d-4b64-ad70-c9f849ad2ba7",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionsRefresh } from "@gram/client/funcs/organizationRemoteSessionsRefresh.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionsRefresh(gram, {
    id: "debf903f-956d-4b64-ad70-c9f849ad2ba7",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionsRefresh failed:", res.error);
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
  useRefreshOrganizationRemoteSessionMutation,
} from "@gram/client/react-query/organizationRemoteSessionsRefresh.js";
```

### Parameters

| Parameter              | Type                                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RefreshOrganizationRemoteSessionRequest](../../models/operations/refreshorganizationremotesessionrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RefreshOrganizationRemoteSessionSecurity](../../models/operations/refreshorganizationremotesessionsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSession](../../models/components/remotesession.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## revoke

Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="revokeOrganizationRemoteSession" method="post" path="/rpc/organizationRemoteSessions.revoke" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.organizationRemoteSessions.revoke({
    id: "5fdebb84-c5ea-4464-9112-8acfc18236cb",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionsRevoke } from "@gram/client/funcs/organizationRemoteSessionsRevoke.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionsRevoke(gram, {
    id: "5fdebb84-c5ea-4464-9112-8acfc18236cb",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("organizationRemoteSessionsRevoke failed:", res.error);
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
  useRevokeOrganizationRemoteSessionMutation,
} from "@gram/client/react-query/organizationRemoteSessionsRevoke.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RevokeOrganizationRemoteSessionRequest](../../models/operations/revokeorganizationremotesessionrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RevokeOrganizationRemoteSessionSecurity](../../models/operations/revokeorganizationremotesessionsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## revokeAll

Revoke (soft-delete) all remote_sessions minted against a remote_session_client in the caller's organization. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="revokeAllOrganizationRemoteSessionClientSessions" method="post" path="/rpc/organizationRemoteSessions.revokeAll" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessions.revokeAll({
    clientId: "bca190ed-9ac4-47c1-ba4c-010a834f6343",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionsRevokeAll } from "@gram/client/funcs/organizationRemoteSessionsRevokeAll.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionsRevokeAll(gram, {
    clientId: "bca190ed-9ac4-47c1-ba4c-010a834f6343",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionsRevokeAll failed:", res.error);
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
  useRevokeAllOrganizationRemoteSessionClientSessionsMutation,
} from "@gram/client/react-query/organizationRemoteSessionsRevokeAll.js";
```

### Parameters

| Parameter              | Type                                                                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RevokeAllOrganizationRemoteSessionClientSessionsRequest](../../models/operations/revokeallorganizationremotesessionclientsessionsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RevokeAllOrganizationRemoteSessionClientSessionsSecurity](../../models/operations/revokeallorganizationremotesessionclientsessionssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RevokeAllRemoteSessionsResult](../../models/components/revokeallremotesessionsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
