# McpEndpoints

## Overview

Managing MCP endpoints, the url-friendly slug identifiers that address MCP servers.

### Available Operations

- [checkSlugAvailability](#checkslugavailability) - checkMcpEndpointSlugAvailability mcpEndpoints
- [create](#create) - createMcpEndpoint mcpEndpoints
- [delete](#delete) - deleteMcpEndpoint mcpEndpoints
- [get](#get) - getMcpEndpoint mcpEndpoints
- [list](#list) - listMcpEndpoints mcpEndpoints
- [update](#update) - updateMcpEndpoint mcpEndpoints

## checkSlugAvailability

Check whether an MCP endpoint slug is available. The uniqueness scope depends on whether a custom_domain_id is provided: platform-domain slugs are checked across all platform-domain endpoints (custom_domain_id IS NULL); custom-domain slugs are checked within the (custom_domain_id, slug) pair. Returns true when the slug is free.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="checkMcpEndpointSlugAvailability" method="get" path="/rpc/mcpEndpoints.checkSlugAvailability" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcpEndpoints.checkSlugAvailability({
    slug: "<value>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { mcpEndpointsCheckSlugAvailability } from "@gram/client/funcs/mcpEndpointsCheckSlugAvailability.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsCheckSlugAvailability(gram, {
    slug: "<value>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("mcpEndpointsCheckSlugAvailability failed:", res.error);
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
  useCheckMcpEndpointSlugAvailability,
  useCheckMcpEndpointSlugAvailabilitySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchCheckMcpEndpointSlugAvailability,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateCheckMcpEndpointSlugAvailability,
  invalidateAllCheckMcpEndpointSlugAvailability,
} from "@gram/client/react-query/mcpEndpointsCheckSlugAvailability.js";
```

### Parameters

| Parameter              | Type                                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CheckMcpEndpointSlugAvailabilityRequest](../../models/operations/checkmcpendpointslugavailabilityrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CheckMcpEndpointSlugAvailabilitySecurity](../../models/operations/checkmcpendpointslugavailabilitysecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[boolean](../../models/.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## create

Create a new MCP endpoint for an MCP server

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createMcpEndpoint" method="post" path="/rpc/mcpEndpoints.create" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcpEndpoints.create({
    createMcpEndpointForm: {
      mcpServerId: "e63f0f94-b02c-44d6-bd59-3db5c78eb608",
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
import { mcpEndpointsCreate } from "@gram/client/funcs/mcpEndpointsCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsCreate(gram, {
    createMcpEndpointForm: {
      mcpServerId: "e63f0f94-b02c-44d6-bd59-3db5c78eb608",
      slug: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("mcpEndpointsCreate failed:", res.error);
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
  useCreateMcpEndpointMutation,
} from "@gram/client/react-query/mcpEndpointsCreate.js";
```

### Parameters

| Parameter              | Type                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateMcpEndpointRequest](../../models/operations/createmcpendpointrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateMcpEndpointSecurity](../../models/operations/createmcpendpointsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.McpEndpoint](../../models/components/mcpendpoint.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## delete

Delete an MCP endpoint

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteMcpEndpoint" method="delete" path="/rpc/mcpEndpoints.delete" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.mcpEndpoints.delete({
    id: "9b26adb6-00fc-4f2e-83e4-04e217c33005",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { mcpEndpointsDelete } from "@gram/client/funcs/mcpEndpointsDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsDelete(gram, {
    id: "9b26adb6-00fc-4f2e-83e4-04e217c33005",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("mcpEndpointsDelete failed:", res.error);
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
  useDeleteMcpEndpointMutation,
} from "@gram/client/react-query/mcpEndpointsDelete.js";
```

### Parameters

| Parameter              | Type                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteMcpEndpointRequest](../../models/operations/deletemcpendpointrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteMcpEndpointSecurity](../../models/operations/deletemcpendpointsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## get

Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getMcpEndpoint" method="get" path="/rpc/mcpEndpoints.get" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcpEndpoints.get();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { mcpEndpointsGet } from "@gram/client/funcs/mcpEndpointsGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsGet(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("mcpEndpointsGet failed:", res.error);
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
  useGetMcpEndpoint,
  useGetMcpEndpointSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetMcpEndpoint,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetMcpEndpoint,
  invalidateAllGetMcpEndpoint,
} from "@gram/client/react-query/mcpEndpointsGet.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetMcpEndpointRequest](../../models/operations/getmcpendpointrequest.md)    | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetMcpEndpointSecurity](../../models/operations/getmcpendpointsecurity.md)  | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.McpEndpoint](../../models/components/mcpendpoint.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listMcpEndpoints" method="get" path="/rpc/mcpEndpoints.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcpEndpoints.list();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { mcpEndpointsList } from "@gram/client/funcs/mcpEndpointsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsList(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("mcpEndpointsList failed:", res.error);
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
  useMcpEndpoints,
  useMcpEndpointsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchMcpEndpoints,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateMcpEndpoints,
  invalidateAllMcpEndpoints,
} from "@gram/client/react-query/mcpEndpointsList.js";
```

### Parameters

| Parameter              | Type                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListMcpEndpointsRequest](../../models/operations/listmcpendpointsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListMcpEndpointsSecurity](../../models/operations/listmcpendpointssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListMcpEndpointsResult](../../models/components/listmcpendpointsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## update

Update an MCP endpoint. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_server_id, and slug fields are required.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateMcpEndpoint" method="post" path="/rpc/mcpEndpoints.update" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.mcpEndpoints.update({
    updateMcpEndpointForm: {
      id: "34af84a7-b505-4ed8-90f2-3279fbef3b89",
      mcpServerId: "e365e808-c8a5-4d4b-bf0a-622a484c6194",
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
import { mcpEndpointsUpdate } from "@gram/client/funcs/mcpEndpointsUpdate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await mcpEndpointsUpdate(gram, {
    updateMcpEndpointForm: {
      id: "34af84a7-b505-4ed8-90f2-3279fbef3b89",
      mcpServerId: "e365e808-c8a5-4d4b-bf0a-622a484c6194",
      slug: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("mcpEndpointsUpdate failed:", res.error);
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
  useUpdateMcpEndpointMutation,
} from "@gram/client/react-query/mcpEndpointsUpdate.js";
```

### Parameters

| Parameter              | Type                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateMcpEndpointRequest](../../models/operations/updatemcpendpointrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateMcpEndpointSecurity](../../models/operations/updatemcpendpointsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.McpEndpoint](../../models/components/mcpendpoint.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
