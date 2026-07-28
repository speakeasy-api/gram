# Agentworkflows

## Overview

OpenAI Responses API compatible endpoint for running agent workflows.

### Available Operations

- [createResponse](#createresponse) - createResponse agentworkflows
- [deleteResponse](#deleteresponse) - deleteResponse agentworkflows
- [getResponse](#getresponse) - getResponse agentworkflows

## createResponse

Create a new agent response. Executes an agent workflow with the provided input and tools.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createResponse" method="post" path="/rpc/workflows.createResponse" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.agentworkflows.createResponse({
    workflowAgentRequest: {
      input: "<value>",
      model: "Taurus",
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
import { agentworkflowsCreateResponse } from "@gram/client/funcs/agentworkflowsCreateResponse.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await agentworkflowsCreateResponse(gram, {
    workflowAgentRequest: {
      input: "<value>",
      model: "Taurus",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("agentworkflowsCreateResponse failed:", res.error);
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
  useAgentworkflowsCreateResponseMutation,
} from "@gram/client/react-query/agentworkflowsCreateResponse.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateResponseRequest](../../models/operations/createresponserequest.md)    | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateResponseSecurity](../../models/operations/createresponsesecurity.md)  | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.WorkflowAgentResponseOutput](../../models/components/workflowagentresponseoutput.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deleteResponse

Deletes any response associated with a given agent run.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteResponse" method="delete" path="/rpc/workflows.deleteResponse" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.agentworkflows.deleteResponse({
    responseId: "<id>",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { agentworkflowsDeleteResponse } from "@gram/client/funcs/agentworkflowsDeleteResponse.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await agentworkflowsDeleteResponse(gram, {
    responseId: "<id>",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("agentworkflowsDeleteResponse failed:", res.error);
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
  useAgentworkflowsDeleteResponseMutation,
} from "@gram/client/react-query/agentworkflowsDeleteResponse.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteResponseRequest](../../models/operations/deleteresponserequest.md)    | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteResponseSecurity](../../models/operations/deleteresponsesecurity.md)  | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getResponse

Get the status of an async agent response by its ID.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getResponse" method="get" path="/rpc/workflows.getResponse" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.agentworkflows.getResponse({
    responseId: "<id>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { agentworkflowsGetResponse } from "@gram/client/funcs/agentworkflowsGetResponse.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await agentworkflowsGetResponse(gram, {
    responseId: "<id>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("agentworkflowsGetResponse failed:", res.error);
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
  useAgentworkflowsGetResponse,
  useAgentworkflowsGetResponseSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchAgentworkflowsGetResponse,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateAgentworkflowsGetResponse,
  invalidateAllAgentworkflowsGetResponse,
} from "@gram/client/react-query/agentworkflowsGetResponse.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetResponseRequest](../../models/operations/getresponserequest.md)          | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetResponseSecurity](../../models/operations/getresponsesecurity.md)        | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.WorkflowAgentResponseOutput](../../models/components/workflowagentresponseoutput.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
