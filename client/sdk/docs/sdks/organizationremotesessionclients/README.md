# OrganizationRemoteSessionClients

## Overview

Manage remote_session_client records from the organization-administrator surface — clients across every project in the caller's organization. client_secret_encrypted is never returned.

### Available Operations

* [create](#create) - createClient organizationRemoteSessionClients
* [createCimd](#createcimd) - createCimdClient organizationRemoteSessionClients
* [delete](#delete) - deleteClient organizationRemoteSessionClients
* [get](#get) - getClient organizationRemoteSessionClients
* [getDeletePreflight](#getdeletepreflight) - getClientDeletePreflight organizationRemoteSessionClients
* [list](#list) - listClients organizationRemoteSessionClients
* [listMcpServers](#listmcpservers) - listClientMcpServers organizationRemoteSessionClients
* [removeFromMcpServer](#removefrommcpserver) - removeClientFromMcpServer organizationRemoteSessionClients
* [update](#update) - updateClient organizationRemoteSessionClients

## create

Register a standalone remote_session_client under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createOrganizationRemoteSessionClient" method="post" path="/rpc/organizationRemoteSessionClients.create" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.create({
    createOrganizationRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "61f009bc-53b7-4a1c-962d-e2f62f10f198",
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
import { organizationRemoteSessionClientsCreate } from "@gram/client/funcs/organizationRemoteSessionClientsCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsCreate(gram, {
    createOrganizationRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "61f009bc-53b7-4a1c-962d-e2f62f10f198",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsCreate failed:", res.error);
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
  useCreateOrganizationRemoteSessionClientMutation
} from "@gram/client/react-query/organizationRemoteSessionClientsCreate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.CreateOrganizationRemoteSessionClientRequest](../../models/operations/createorganizationremotesessionclientrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.CreateOrganizationRemoteSessionClientSecurity](../../models/operations/createorganizationremotesessionclientsecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## createCimd

Register a standalone remote_session_client in Client ID Metadata Document (CIMD) mode under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. Gram generates the client_id and hosts the metadata document; the issuer must advertise client_id_metadata_document_supported. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createCimdOrganizationRemoteSessionClient" method="post" path="/rpc/organizationRemoteSessionClients.createCimd" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.createCimd({
    createCimdOrganizationRemoteSessionClientForm: {
      remoteSessionIssuerId: "d26acb2a-13f2-4784-955a-a0658dbb938d",
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
import { organizationRemoteSessionClientsCreateCimd } from "@gram/client/funcs/organizationRemoteSessionClientsCreateCimd.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsCreateCimd(gram, {
    createCimdOrganizationRemoteSessionClientForm: {
      remoteSessionIssuerId: "d26acb2a-13f2-4784-955a-a0658dbb938d",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsCreateCimd failed:", res.error);
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
  useCreateCimdOrganizationRemoteSessionClientMutation
} from "@gram/client/react-query/organizationRemoteSessionClientsCreateCimd.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.CreateCimdOrganizationRemoteSessionClientRequest](../../models/operations/createcimdorganizationremotesessionclientrequest.md)                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.CreateCimdOrganizationRemoteSessionClientSecurity](../../models/operations/createcimdorganizationremotesessionclientsecurity.md)                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## delete

Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteOrganizationRemoteSessionClient" method="delete" path="/rpc/organizationRemoteSessionClients.delete" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.organizationRemoteSessionClients.delete({
    id: "1d758bb6-ef22-4047-b7cd-a26fe8f05dfe",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionClientsDelete } from "@gram/client/funcs/organizationRemoteSessionClientsDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsDelete(gram, {
    id: "1d758bb6-ef22-4047-b7cd-a26fe8f05dfe",
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("organizationRemoteSessionClientsDelete failed:", res.error);
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
  useDeleteOrganizationRemoteSessionClientMutation
} from "@gram/client/react-query/organizationRemoteSessionClientsDelete.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteOrganizationRemoteSessionClientRequest](../../models/operations/deleteorganizationremotesessionclientrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.DeleteOrganizationRemoteSessionClientSecurity](../../models/operations/deleteorganizationremotesessionclientsecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
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

Get a remote_session_client in the caller's organization by id. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getOrganizationRemoteSessionClient" method="get" path="/rpc/organizationRemoteSessionClients.get" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.get({
    id: "5ca0426f-30f3-49c5-8b75-716fd349e47b",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionClientsGet } from "@gram/client/funcs/organizationRemoteSessionClientsGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsGet(gram, {
    id: "5ca0426f-30f3-49c5-8b75-716fd349e47b",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsGet failed:", res.error);
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
  useOrganizationRemoteSessionClient,
  useOrganizationRemoteSessionClientSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionClient,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionClient,
  invalidateAllOrganizationRemoteSessionClient,
} from "@gram/client/react-query/organizationRemoteSessionClientsGet.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetOrganizationRemoteSessionClientRequest](../../models/operations/getorganizationremotesessionclientrequest.md)                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetOrganizationRemoteSessionClientSecurity](../../models/operations/getorganizationremotesessionclientsecurity.md)                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getDeletePreflight

Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getOrganizationRemoteSessionClientDeletePreflight" method="get" path="/rpc/organizationRemoteSessionClients.getDeletePreflight" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.getDeletePreflight({
    id: "e1f42171-6f72-4604-8a8f-46ad41ca8150",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionClientsGetDeletePreflight } from "@gram/client/funcs/organizationRemoteSessionClientsGetDeletePreflight.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsGetDeletePreflight(gram, {
    id: "e1f42171-6f72-4604-8a8f-46ad41ca8150",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsGetDeletePreflight failed:", res.error);
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
  useOrganizationRemoteSessionClientDeletePreflight,
  useOrganizationRemoteSessionClientDeletePreflightSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionClientDeletePreflight,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionClientDeletePreflight,
  invalidateAllOrganizationRemoteSessionClientDeletePreflight,
} from "@gram/client/react-query/organizationRemoteSessionClientsGetDeletePreflight.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetOrganizationRemoteSessionClientDeletePreflightRequest](../../models/operations/getorganizationremotesessionclientdeletepreflightrequest.md)                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetOrganizationRemoteSessionClientDeletePreflightSecurity](../../models/operations/getorganizationremotesessionclientdeletepreflightsecurity.md)                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.OrganizationClientDeletePreflight](../../models/components/organizationclientdeletepreflight.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## list

List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listOrganizationRemoteSessionClients" method="get" path="/rpc/organizationRemoteSessionClients.list" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.list({
    issuerId: "7f11da21-036c-4fc5-beb2-c15362b44146",
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
import { organizationRemoteSessionClientsList } from "@gram/client/funcs/organizationRemoteSessionClientsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsList(gram, {
    issuerId: "7f11da21-036c-4fc5-beb2-c15362b44146",
  });
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
    console.log(page);
  }
  } else {
    console.log("organizationRemoteSessionClientsList failed:", res.error);
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
  useOrganizationRemoteSessionClients,
  useOrganizationRemoteSessionClientsSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useOrganizationRemoteSessionClientsInfinite,
  useOrganizationRemoteSessionClientsInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionClients,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionClients,
  invalidateAllOrganizationRemoteSessionClients,
} from "@gram/client/react-query/organizationRemoteSessionClientsList.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListOrganizationRemoteSessionClientsRequest](../../models/operations/listorganizationremotesessionclientsrequest.md)                                               | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListOrganizationRemoteSessionClientsSecurity](../../models/operations/listorganizationremotesessionclientssecurity.md)                                             | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListOrganizationRemoteSessionClientsResponse](../../models/operations/listorganizationremotesessionclientsresponse.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listMcpServers

List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listOrganizationRemoteSessionClientMcpServers" method="get" path="/rpc/organizationRemoteSessionClients.listMcpServers" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.listMcpServers({
    clientId: "89a0ca44-8d53-4af6-b342-85d7d6dba8cd",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionClientsListMcpServers } from "@gram/client/funcs/organizationRemoteSessionClientsListMcpServers.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsListMcpServers(gram, {
    clientId: "89a0ca44-8d53-4af6-b342-85d7d6dba8cd",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsListMcpServers failed:", res.error);
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
  useOrganizationRemoteSessionClientMcpServers,
  useOrganizationRemoteSessionClientMcpServersSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOrganizationRemoteSessionClientMcpServers,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOrganizationRemoteSessionClientMcpServers,
  invalidateAllOrganizationRemoteSessionClientMcpServers,
} from "@gram/client/react-query/organizationRemoteSessionClientsListMcpServers.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListOrganizationRemoteSessionClientMcpServersRequest](../../models/operations/listorganizationremotesessionclientmcpserversrequest.md)                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListOrganizationRemoteSessionClientMcpServersSecurity](../../models/operations/listorganizationremotesessionclientmcpserverssecurity.md)                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListOrganizationMcpServersResult](../../models/components/listorganizationmcpserversresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## removeFromMcpServer

Detach a remote_session_client from an MCP server (clears the MCP server's user_session_issuer link) in the caller's organization. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="removeOrganizationRemoteSessionClientFromMcpServer" method="post" path="/rpc/organizationRemoteSessionClients.removeFromMcpServer" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.organizationRemoteSessionClients.removeFromMcpServer({
    removeClientFromMcpServerRequestBody: {
      clientId: "b8469a49-74d5-4a05-9846-c0e1d2985343",
      mcpServerId: "67f4ce51-85c5-4d1d-aaae-81da7cc4c225",
    },
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionClientsRemoveFromMcpServer } from "@gram/client/funcs/organizationRemoteSessionClientsRemoveFromMcpServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsRemoveFromMcpServer(gram, {
    removeClientFromMcpServerRequestBody: {
      clientId: "b8469a49-74d5-4a05-9846-c0e1d2985343",
      mcpServerId: "67f4ce51-85c5-4d1d-aaae-81da7cc4c225",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("organizationRemoteSessionClientsRemoveFromMcpServer failed:", res.error);
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
  useRemoveOrganizationRemoteSessionClientFromMcpServerMutation
} from "@gram/client/react-query/organizationRemoteSessionClientsRemoveFromMcpServer.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.RemoveOrganizationRemoteSessionClientFromMcpServerRequest](../../models/operations/removeorganizationremotesessionclientfrommcpserverrequest.md)                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.RemoveOrganizationRemoteSessionClientFromMcpServerSecurity](../../models/operations/removeorganizationremotesessionclientfrommcpserversecurity.md)                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
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

## update

Update a remote_session_client's non-secret fields in the caller's organization. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateOrganizationRemoteSessionClient" method="post" path="/rpc/organizationRemoteSessionClients.update" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.organizationRemoteSessionClients.update({
    updateRemoteSessionClientForm: {
      id: "2b7c2a8b-15ee-4195-8c17-432168bdfb4f",
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
import { organizationRemoteSessionClientsUpdate } from "@gram/client/funcs/organizationRemoteSessionClientsUpdate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await organizationRemoteSessionClientsUpdate(gram, {
    updateRemoteSessionClientForm: {
      id: "2b7c2a8b-15ee-4195-8c17-432168bdfb4f",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("organizationRemoteSessionClientsUpdate failed:", res.error);
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
  useUpdateOrganizationRemoteSessionClientMutation
} from "@gram/client/react-query/organizationRemoteSessionClientsUpdate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.UpdateOrganizationRemoteSessionClientRequest](../../models/operations/updateorganizationremotesessionclientrequest.md)                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.UpdateOrganizationRemoteSessionClientSecurity](../../models/operations/updateorganizationremotesessionclientsecurity.md)                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |