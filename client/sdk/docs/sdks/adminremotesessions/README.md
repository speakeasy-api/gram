# AdminRemoteSessions

## Overview

Platform-admin management of global remote_session_issuer / remote_session_client records — shared across every organization (project_id NULL, organization_id NULL). Speakeasy-staff only; every method requires the platform-admin flag.

### Available Operations

- [createGlobalClient](#createglobalclient) - createGlobalClient adminRemoteSessions
- [createGlobalIssuer](#createglobalissuer) - createGlobalIssuer adminRemoteSessions
- [deleteGlobalClient](#deleteglobalclient) - deleteGlobalClient adminRemoteSessions
- [deleteGlobalIssuer](#deleteglobalissuer) - deleteGlobalIssuer adminRemoteSessions
- [getGlobalClient](#getglobalclient) - getGlobalClient adminRemoteSessions
- [getGlobalIssuer](#getglobalissuer) - getGlobalIssuer adminRemoteSessions
- [listGlobalClients](#listglobalclients) - listGlobalClients adminRemoteSessions
- [listGlobalIssuers](#listglobalissuers) - listGlobalIssuers adminRemoteSessions
- [updateGlobalClient](#updateglobalclient) - updateGlobalClient adminRemoteSessions
- [updateGlobalIssuer](#updateglobalissuer) - updateGlobalIssuer adminRemoteSessions

## createGlobalClient

Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createGlobalRemoteSessionClient" method="post" path="/rpc/adminRemoteSessions.createGlobalClient" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.createGlobalClient({
    createGlobalRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "6f267ffc-c6f0-4622-88cc-2899f4e1190f",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsCreateGlobalClient } from "@gram/client/funcs/adminRemoteSessionsCreateGlobalClient.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsCreateGlobalClient(gram, {
    createGlobalRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "6f267ffc-c6f0-4622-88cc-2899f4e1190f",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsCreateGlobalClient failed:", res.error);
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
  useCreateGlobalRemoteSessionClientMutation,
} from "@gram/client/react-query/adminRemoteSessionsCreateGlobalClient.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateGlobalRemoteSessionClientRequest](../../models/operations/createglobalremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateGlobalRemoteSessionClientSecurity](../../models/operations/createglobalremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## createGlobalIssuer

Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createGlobalRemoteSessionIssuer" method="post" path="/rpc/adminRemoteSessions.createGlobalIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.createGlobalIssuer({
    createRemoteSessionIssuerForm: {
      issuer: "diners_club",
      slug: "<value>",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsCreateGlobalIssuer } from "@gram/client/funcs/adminRemoteSessionsCreateGlobalIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsCreateGlobalIssuer(gram, {
    createRemoteSessionIssuerForm: {
      issuer: "diners_club",
      slug: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsCreateGlobalIssuer failed:", res.error);
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
  useCreateGlobalRemoteSessionIssuerMutation,
} from "@gram/client/react-query/adminRemoteSessionsCreateGlobalIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateGlobalRemoteSessionIssuerRequest](../../models/operations/createglobalremotesessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateGlobalRemoteSessionIssuerSecurity](../../models/operations/createglobalremotesessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deleteGlobalClient

Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteGlobalRemoteSessionClient" method="delete" path="/rpc/adminRemoteSessions.deleteGlobalClient" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.adminRemoteSessions.deleteGlobalClient({
    id: "4bd8478b-172d-42a8-a49f-5bdf730cbda8",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsDeleteGlobalClient } from "@gram/client/funcs/adminRemoteSessionsDeleteGlobalClient.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsDeleteGlobalClient(gram, {
    id: "4bd8478b-172d-42a8-a49f-5bdf730cbda8",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("adminRemoteSessionsDeleteGlobalClient failed:", res.error);
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
  useDeleteGlobalRemoteSessionClientMutation,
} from "@gram/client/react-query/adminRemoteSessionsDeleteGlobalClient.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteGlobalRemoteSessionClientRequest](../../models/operations/deleteglobalremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteGlobalRemoteSessionClientSecurity](../../models/operations/deleteglobalremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
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

## deleteGlobalIssuer

Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteGlobalRemoteSessionIssuer" method="delete" path="/rpc/adminRemoteSessions.deleteGlobalIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.adminRemoteSessions.deleteGlobalIssuer({
    id: "8d57ae29-5f45-4a11-bab4-803788203df3",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsDeleteGlobalIssuer } from "@gram/client/funcs/adminRemoteSessionsDeleteGlobalIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsDeleteGlobalIssuer(gram, {
    id: "8d57ae29-5f45-4a11-bab4-803788203df3",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("adminRemoteSessionsDeleteGlobalIssuer failed:", res.error);
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
  useDeleteGlobalRemoteSessionIssuerMutation,
} from "@gram/client/react-query/adminRemoteSessionsDeleteGlobalIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteGlobalRemoteSessionIssuerRequest](../../models/operations/deleteglobalremotesessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteGlobalRemoteSessionIssuerSecurity](../../models/operations/deleteglobalremotesessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
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

## getGlobalClient

Get a global remote_session_client by id. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getGlobalRemoteSessionClient" method="get" path="/rpc/adminRemoteSessions.getGlobalClient" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.getGlobalClient({
    id: "a32e7fe3-ad88-4843-9e9b-d029eca73d54",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsGetGlobalClient } from "@gram/client/funcs/adminRemoteSessionsGetGlobalClient.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsGetGlobalClient(gram, {
    id: "a32e7fe3-ad88-4843-9e9b-d029eca73d54",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsGetGlobalClient failed:", res.error);
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
  useGlobalRemoteSessionClient,
  useGlobalRemoteSessionClientSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGlobalRemoteSessionClient,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGlobalRemoteSessionClient,
  invalidateAllGlobalRemoteSessionClient,
} from "@gram/client/react-query/adminRemoteSessionsGetGlobalClient.js";
```

### Parameters

| Parameter              | Type                                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetGlobalRemoteSessionClientRequest](../../models/operations/getglobalremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetGlobalRemoteSessionClientSecurity](../../models/operations/getglobalremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getGlobalIssuer

Get a global remote_session_issuer by id. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getGlobalRemoteSessionIssuer" method="get" path="/rpc/adminRemoteSessions.getGlobalIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.getGlobalIssuer({
    id: "a899ca0f-2b91-4146-9aff-dbd0c0087f45",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsGetGlobalIssuer } from "@gram/client/funcs/adminRemoteSessionsGetGlobalIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsGetGlobalIssuer(gram, {
    id: "a899ca0f-2b91-4146-9aff-dbd0c0087f45",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsGetGlobalIssuer failed:", res.error);
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
  useGlobalRemoteSessionIssuer,
  useGlobalRemoteSessionIssuerSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGlobalRemoteSessionIssuer,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGlobalRemoteSessionIssuer,
  invalidateAllGlobalRemoteSessionIssuer,
} from "@gram/client/react-query/adminRemoteSessionsGetGlobalIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetGlobalRemoteSessionIssuerRequest](../../models/operations/getglobalremotesessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetGlobalRemoteSessionIssuerSecurity](../../models/operations/getglobalremotesessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listGlobalClients

List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listGlobalRemoteSessionClients" method="get" path="/rpc/adminRemoteSessions.listGlobalClients" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.listGlobalClients({
    remoteSessionIssuerId: "7c1fa209-8b42-46d5-9dae-1898e2a1e3bf",
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
import { adminRemoteSessionsListGlobalClients } from "@gram/client/funcs/adminRemoteSessionsListGlobalClients.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsListGlobalClients(gram, {
    remoteSessionIssuerId: "7c1fa209-8b42-46d5-9dae-1898e2a1e3bf",
  });
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
      console.log(page);
    }
  } else {
    console.log("adminRemoteSessionsListGlobalClients failed:", res.error);
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
  useGlobalRemoteSessionClients,
  useGlobalRemoteSessionClientsSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useGlobalRemoteSessionClientsInfinite,
  useGlobalRemoteSessionClientsInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGlobalRemoteSessionClients,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGlobalRemoteSessionClients,
  invalidateAllGlobalRemoteSessionClients,
} from "@gram/client/react-query/adminRemoteSessionsListGlobalClients.js";
```

### Parameters

| Parameter              | Type                                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListGlobalRemoteSessionClientsRequest](../../models/operations/listglobalremotesessionclientsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListGlobalRemoteSessionClientsSecurity](../../models/operations/listglobalremotesessionclientssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListGlobalRemoteSessionClientsResponse](../../models/operations/listglobalremotesessionclientsresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listGlobalIssuers

List global remote_session_issuers. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listGlobalRemoteSessionIssuers" method="get" path="/rpc/adminRemoteSessions.listGlobalIssuers" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.listGlobalIssuers();

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
import { adminRemoteSessionsListGlobalIssuers } from "@gram/client/funcs/adminRemoteSessionsListGlobalIssuers.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsListGlobalIssuers(gram);
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
      console.log(page);
    }
  } else {
    console.log("adminRemoteSessionsListGlobalIssuers failed:", res.error);
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
  useGlobalRemoteSessionIssuers,
  useGlobalRemoteSessionIssuersSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useGlobalRemoteSessionIssuersInfinite,
  useGlobalRemoteSessionIssuersInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGlobalRemoteSessionIssuers,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGlobalRemoteSessionIssuers,
  invalidateAllGlobalRemoteSessionIssuers,
} from "@gram/client/react-query/adminRemoteSessionsListGlobalIssuers.js";
```

### Parameters

| Parameter              | Type                                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListGlobalRemoteSessionIssuersRequest](../../models/operations/listglobalremotesessionissuersrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListGlobalRemoteSessionIssuersSecurity](../../models/operations/listglobalremotesessionissuerssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListGlobalRemoteSessionIssuersResponse](../../models/operations/listglobalremotesessionissuersresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateGlobalClient

Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateGlobalRemoteSessionClient" method="post" path="/rpc/adminRemoteSessions.updateGlobalClient" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.updateGlobalClient({
    updateRemoteSessionClientForm: {
      id: "9988f3a1-fc36-4a9f-b68e-b087538c5460",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsUpdateGlobalClient } from "@gram/client/funcs/adminRemoteSessionsUpdateGlobalClient.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsUpdateGlobalClient(gram, {
    updateRemoteSessionClientForm: {
      id: "9988f3a1-fc36-4a9f-b68e-b087538c5460",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsUpdateGlobalClient failed:", res.error);
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
  useUpdateGlobalRemoteSessionClientMutation,
} from "@gram/client/react-query/adminRemoteSessionsUpdateGlobalClient.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateGlobalRemoteSessionClientRequest](../../models/operations/updateglobalremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateGlobalRemoteSessionClientSecurity](../../models/operations/updateglobalremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateGlobalIssuer

Update a global remote_session_issuer. Requires platform admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateGlobalRemoteSessionIssuer" method="post" path="/rpc/adminRemoteSessions.updateGlobalIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.adminRemoteSessions.updateGlobalIssuer({
    updateRemoteSessionIssuerForm: {
      id: "6df977ce-98f8-45c9-926e-7c2b769aaf35",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { adminRemoteSessionsUpdateGlobalIssuer } from "@gram/client/funcs/adminRemoteSessionsUpdateGlobalIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await adminRemoteSessionsUpdateGlobalIssuer(gram, {
    updateRemoteSessionIssuerForm: {
      id: "6df977ce-98f8-45c9-926e-7c2b769aaf35",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("adminRemoteSessionsUpdateGlobalIssuer failed:", res.error);
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
  useUpdateGlobalRemoteSessionIssuerMutation,
} from "@gram/client/react-query/adminRemoteSessionsUpdateGlobalIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateGlobalRemoteSessionIssuerRequest](../../models/operations/updateglobalremotesessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateGlobalRemoteSessionIssuerSecurity](../../models/operations/updateglobalremotesessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
