# OrganizationRemoteSessionIssuers

## Overview

Manage organization-level remote_session_issuer records — cross-project upstream Authorization Server identity records inherited by every project in the organization.

### Available Operations

* [create](#create) - createIssuer organizationRemoteSessionIssuers
* [delete](#delete) - deleteIssuer organizationRemoteSessionIssuers
* [get](#get) - getIssuer organizationRemoteSessionIssuers
* [getDeletePreflight](#getdeletepreflight) - getIssuerDeletePreflight organizationRemoteSessionIssuers
* [list](#list) - listIssuers organizationRemoteSessionIssuers
* [move](#move) - moveIssuer organizationRemoteSessionIssuers
* [update](#update) - updateIssuer organizationRemoteSessionIssuers

## create

Create a remote_session_issuer in the caller's organization. With no project_id the issuer is organization-level (project_id NULL, inherited by every project); with a project_id (which must belong to the organization) it is project-specific. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createOrganizationRemoteSessionIssuer" method="post" path="/rpc/organizationRemoteSessionIssuers.create" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.create({
    createIssuerRequestBody: {
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
import { organizationRemoteSessionIssuersCreate } from "@gram/client/funcs/organizationRemoteSessionIssuersCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersCreate(gram, {
    createIssuerRequestBody: {
      issuer: "diners_club",
      slug: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionIssuersCreate failed:", res.error);
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
  useCreateOrganizationRemoteSessionIssuerMutation
} from "@gram/client/react-query/organizationRemoteSessionIssuersCreate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.CreateOrganizationRemoteSessionIssuerRequest](../../models/operations/createorganizationremotesessionissuerrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.CreateOrganizationRemoteSessionIssuerSecurity](../../models/operations/createorganizationremotesessionissuersecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## delete

Soft-delete any remote_session_issuer (organizational or project-specific) in the caller's organization. Blocked when any remote_session_clients still reference it. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteOrganizationRemoteSessionIssuer" method="delete" path="/rpc/organizationRemoteSessionIssuers.delete" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.organizationRemoteSessionIssuers.delete({
    id: "bfa90e96-73e4-4ebd-9db4-a49e5bd23cea",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionIssuersDelete } from "@gram/client/funcs/organizationRemoteSessionIssuersDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersDelete(gram, {
    id: "bfa90e96-73e4-4ebd-9db4-a49e5bd23cea",
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("organizationRemoteSessionIssuersDelete failed:", res.error);
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
  useDeleteOrganizationRemoteSessionIssuerMutation
} from "@gram/client/react-query/organizationRemoteSessionIssuersDelete.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteOrganizationRemoteSessionIssuerRequest](../../models/operations/deleteorganizationremotesessionissuerrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.DeleteOrganizationRemoteSessionIssuerSecurity](../../models/operations/deleteorganizationremotesessionissuersecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
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

## get

Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getOrganizationRemoteSessionIssuer" method="get" path="/rpc/organizationRemoteSessionIssuers.get" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.get({
    id: "55d3fd51-1410-48f5-841a-1d962a2b6844",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionIssuersGet } from "@gram/client/funcs/organizationRemoteSessionIssuersGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersGet(gram, {
    id: "55d3fd51-1410-48f5-841a-1d962a2b6844",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionIssuersGet failed:", res.error);
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
  useOrganizationRemoteSessionIssuer,
  useOrganizationRemoteSessionIssuerSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionIssuer,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionIssuer,
  invalidateAllOrganizationRemoteSessionIssuer,
} from "@gram/client/react-query/organizationRemoteSessionIssuersGet.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetOrganizationRemoteSessionIssuerRequest](../../models/operations/getorganizationremotesessionissuerrequest.md)                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetOrganizationRemoteSessionIssuerSecurity](../../models/operations/getorganizationremotesessionissuersecurity.md)                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getDeletePreflight

Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getOrganizationRemoteSessionIssuerDeletePreflight" method="get" path="/rpc/organizationRemoteSessionIssuers.getDeletePreflight" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.getDeletePreflight({
    id: "ebb2300c-95e5-4242-aa05-6db8c4ff86e6",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionIssuersGetDeletePreflight } from "@gram/client/funcs/organizationRemoteSessionIssuersGetDeletePreflight.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersGetDeletePreflight(gram, {
    id: "ebb2300c-95e5-4242-aa05-6db8c4ff86e6",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionIssuersGetDeletePreflight failed:", res.error);
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
  useOrganizationRemoteSessionIssuerDeletePreflight,
  useOrganizationRemoteSessionIssuerDeletePreflightSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionIssuerDeletePreflight,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionIssuerDeletePreflight,
  invalidateAllOrganizationRemoteSessionIssuerDeletePreflight,
} from "@gram/client/react-query/organizationRemoteSessionIssuersGetDeletePreflight.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetOrganizationRemoteSessionIssuerDeletePreflightRequest](../../models/operations/getorganizationremotesessionissuerdeletepreflightrequest.md)                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetOrganizationRemoteSessionIssuerDeletePreflightSecurity](../../models/operations/getorganizationremotesessionissuerdeletepreflightsecurity.md)                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.OrganizationIssuerDeletePreflight](../../models/components/organizationissuerdeletepreflight.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## list

List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listOrganizationRemoteSessionIssuers" method="get" path="/rpc/organizationRemoteSessionIssuers.list" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.list();

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
import { organizationRemoteSessionIssuersList } from "@gram/client/funcs/organizationRemoteSessionIssuersList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersList(gram);
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
    console.log(page);
  }
  } else {
    console.log("organizationRemoteSessionIssuersList failed:", res.error);
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
  useOrganizationRemoteSessionIssuers,
  useOrganizationRemoteSessionIssuersSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useOrganizationRemoteSessionIssuersInfinite,
  useOrganizationRemoteSessionIssuersInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionIssuers,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionIssuers,
  invalidateAllOrganizationRemoteSessionIssuers,
} from "@gram/client/react-query/organizationRemoteSessionIssuersList.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListOrganizationRemoteSessionIssuersRequest](../../models/operations/listorganizationremotesessionissuersrequest.md)                                               | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListOrganizationRemoteSessionIssuersSecurity](../../models/operations/listorganizationremotesessionissuerssecurity.md)                                             | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListOrganizationRemoteSessionIssuersResponse](../../models/operations/listorganizationremotesessionissuersresponse.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## move

Re-scope a remote_session_issuer in the caller's organization: provide a project_id (which must belong to the organization) to make it project-specific, or omit it to make it organization-level (project_id NULL, inherited by every project). Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="moveOrganizationRemoteSessionIssuer" method="post" path="/rpc/organizationRemoteSessionIssuers.move" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.move({
    moveIssuerRequestBody: {
      id: "a2f073a6-f769-4f7f-ad18-960bb3ce5070",
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
import { organizationRemoteSessionIssuersMove } from "@gram/client/funcs/organizationRemoteSessionIssuersMove.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersMove(gram, {
    moveIssuerRequestBody: {
      id: "a2f073a6-f769-4f7f-ad18-960bb3ce5070",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionIssuersMove failed:", res.error);
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
  useMoveOrganizationRemoteSessionIssuerMutation
} from "@gram/client/react-query/organizationRemoteSessionIssuersMove.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.MoveOrganizationRemoteSessionIssuerRequest](../../models/operations/moveorganizationremotesessionissuerrequest.md)                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.MoveOrganizationRemoteSessionIssuerSecurity](../../models/operations/moveorganizationremotesessionissuersecurity.md)                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## update

Update any remote_session_issuer (organizational or project-specific) in the caller's organization. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateOrganizationRemoteSessionIssuer" method="post" path="/rpc/organizationRemoteSessionIssuers.update" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionIssuers.update({
    updateRemoteSessionIssuerForm: {
      id: "f92575af-75b9-49bf-b738-df46e967897e",
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
import { organizationRemoteSessionIssuersUpdate } from "@gram/client/funcs/organizationRemoteSessionIssuersUpdate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionIssuersUpdate(gram, {
    updateRemoteSessionIssuerForm: {
      id: "f92575af-75b9-49bf-b738-df46e967897e",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionIssuersUpdate failed:", res.error);
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
  useUpdateOrganizationRemoteSessionIssuerMutation
} from "@gram/client/react-query/organizationRemoteSessionIssuersUpdate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.UpdateOrganizationRemoteSessionIssuerRequest](../../models/operations/updateorganizationremotesessionissuerrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.UpdateOrganizationRemoteSessionIssuerSecurity](../../models/operations/updateorganizationremotesessionissuersecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |