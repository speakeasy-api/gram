# AssistantMemories

## Overview

Manage assistant memory records.

### Available Operations

- [delete](#delete) - deleteAssistantMemory assistantMemories
- [get](#get) - getAssistantMemory assistantMemories
- [list](#list) - listAssistantMemories assistantMemories

## delete

Delete an assistant memory by ID.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteAssistantMemory" method="delete" path="/rpc/assistantMemories.delete" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.assistantMemories.delete({
    id: "beb39ec7-32f7-4d96-955e-bdc94a92a67a",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { assistantMemoriesDelete } from "@gram/client/funcs/assistantMemoriesDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await assistantMemoriesDelete(gram, {
    id: "beb39ec7-32f7-4d96-955e-bdc94a92a67a",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("assistantMemoriesDelete failed:", res.error);
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
  useAssistantMemoriesDeleteMutation,
} from "@gram/client/react-query/assistantMemoriesDelete.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteAssistantMemoryRequest](../../models/operations/deleteassistantmemoryrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteAssistantMemorySecurity](../../models/operations/deleteassistantmemorysecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## get

Get an assistant memory by ID.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getAssistantMemory" method="get" path="/rpc/assistantMemories.get" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assistantMemories.get({
    id: "d55875b5-a05d-421d-b77d-3b55c98f9193",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { assistantMemoriesGet } from "@gram/client/funcs/assistantMemoriesGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await assistantMemoriesGet(gram, {
    id: "d55875b5-a05d-421d-b77d-3b55c98f9193",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("assistantMemoriesGet failed:", res.error);
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
  useGetAssistantMemory,
  useGetAssistantMemorySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetAssistantMemory,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetAssistantMemory,
  invalidateAllGetAssistantMemory,
} from "@gram/client/react-query/assistantMemoriesGet.js";
```

### Parameters

| Parameter              | Type                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetAssistantMemoryRequest](../../models/operations/getassistantmemoryrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetAssistantMemorySecurity](../../models/operations/getassistantmemorysecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AssistantMemory](../../models/components/assistantmemory.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List assistant memories for an assistant.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listAssistantMemories" method="get" path="/rpc/assistantMemories.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assistantMemories.list({
    assistantId: "56bcc863-cc81-4d15-92ee-28eb89e8930f",
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
import { assistantMemoriesList } from "@gram/client/funcs/assistantMemoriesList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await assistantMemoriesList(gram, {
    assistantId: "56bcc863-cc81-4d15-92ee-28eb89e8930f",
  });
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
      console.log(page);
    }
  } else {
    console.log("assistantMemoriesList failed:", res.error);
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
  useListAssistantMemories,
  useListAssistantMemoriesSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useListAssistantMemoriesInfinite,
  useListAssistantMemoriesInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListAssistantMemories,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListAssistantMemories,
  invalidateAllListAssistantMemories,
} from "@gram/client/react-query/assistantMemoriesList.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListAssistantMemoriesRequest](../../models/operations/listassistantmemoriesrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListAssistantMemoriesSecurity](../../models/operations/listassistantmemoriessecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListAssistantMemoriesResponse](../../models/operations/listassistantmemoriesresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
