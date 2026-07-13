# Triggers

## Overview

Manage project trigger instances and static trigger definitions.

### Available Operations

- [create](#create) - createTriggerInstance triggers
- [delete](#delete) - deleteTriggerInstance triggers
- [get](#get) - getTriggerInstance triggers
- [list](#list) - listTriggerInstances triggers
- [listDefinitions](#listdefinitions) - listTriggerDefinitions triggers
- [pause](#pause) - pauseTriggerInstance triggers
- [resume](#resume) - resumeTriggerInstance triggers
- [update](#update) - updateTriggerInstance triggers

## create

Create a trigger instance.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createTriggerInstance" method="post" path="/rpc/triggers.create" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.create({
    createTriggerInstanceForm: {
      config: {
        key: "<value>",
        key1: "<value>",
      },
      definitionSlug: "<value>",
      name: "<value>",
      targetDisplay: "<value>",
      targetKind: "assistant",
      targetRef: "<value>",
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
import { triggersCreate } from "@gram/client/funcs/triggersCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersCreate(gram, {
    createTriggerInstanceForm: {
      config: {
        key: "<value>",
        key1: "<value>",
      },
      definitionSlug: "<value>",
      name: "<value>",
      targetDisplay: "<value>",
      targetKind: "assistant",
      targetRef: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersCreate failed:", res.error);
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
  useCreateTriggerMutation,
} from "@gram/client/react-query/triggersCreate.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateTriggerInstanceRequest](../../models/operations/createtriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateTriggerInstanceSecurity](../../models/operations/createtriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TriggerInstance](../../models/components/triggerinstance.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## delete

Delete a trigger instance.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteTriggerInstance" method="delete" path="/rpc/triggers.delete" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.triggers.delete({
    id: "69797cf6-f8b0-4412-9bc6-588bd8adbea4",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersDelete } from "@gram/client/funcs/triggersDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersDelete(gram, {
    id: "69797cf6-f8b0-4412-9bc6-588bd8adbea4",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("triggersDelete failed:", res.error);
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
  useDeleteTriggerMutation,
} from "@gram/client/react-query/triggersDelete.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteTriggerInstanceRequest](../../models/operations/deletetriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteTriggerInstanceSecurity](../../models/operations/deletetriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
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

Get a trigger instance by ID.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getTriggerInstance" method="get" path="/rpc/triggers.get" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.get({
    id: "7a2ae90d-2bcd-4905-8efa-65f2597b6f1d",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersGet } from "@gram/client/funcs/triggersGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersGet(gram, {
    id: "7a2ae90d-2bcd-4905-8efa-65f2597b6f1d",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersGet failed:", res.error);
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
  useTrigger,
  useTriggerSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchTrigger,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateTrigger,
  invalidateAllTrigger,
} from "@gram/client/react-query/triggersGet.js";
```

### Parameters

| Parameter              | Type                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetTriggerInstanceRequest](../../models/operations/gettriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetTriggerInstanceSecurity](../../models/operations/gettriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TriggerInstance](../../models/components/triggerinstance.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List trigger instances for the current project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listTriggerInstances" method="get" path="/rpc/triggers.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.list();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersList } from "@gram/client/funcs/triggersList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersList(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersList failed:", res.error);
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
  useTriggers,
  useTriggersSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchTriggers,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateTriggers,
  invalidateAllTriggers,
} from "@gram/client/react-query/triggersList.js";
```

### Parameters

| Parameter              | Type                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListTriggerInstancesRequest](../../models/operations/listtriggerinstancesrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListTriggerInstancesSecurity](../../models/operations/listtriggerinstancessecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListTriggerInstancesResult](../../models/components/listtriggerinstancesresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listDefinitions

List static trigger definitions available to a project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listTriggerDefinitions" method="get" path="/rpc/triggers.listDefinitions" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.listDefinitions();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersListDefinitions } from "@gram/client/funcs/triggersListDefinitions.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersListDefinitions(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersListDefinitions failed:", res.error);
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
  useTriggerDefinitions,
  useTriggerDefinitionsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchTriggerDefinitions,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateTriggerDefinitions,
  invalidateAllTriggerDefinitions,
} from "@gram/client/react-query/triggersListDefinitions.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListTriggerDefinitionsRequest](../../models/operations/listtriggerdefinitionsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListTriggerDefinitionsSecurity](../../models/operations/listtriggerdefinitionssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListTriggerDefinitionsResult](../../models/components/listtriggerdefinitionsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## pause

Pause a trigger instance.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="pauseTriggerInstance" method="post" path="/rpc/triggers.pause" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.pause({
    id: "f9535c1a-e0ed-46e4-8fd1-82aed449f815",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersPause } from "@gram/client/funcs/triggersPause.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersPause(gram, {
    id: "f9535c1a-e0ed-46e4-8fd1-82aed449f815",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersPause failed:", res.error);
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
  usePauseTriggerMutation,
} from "@gram/client/react-query/triggersPause.js";
```

### Parameters

| Parameter              | Type                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.PauseTriggerInstanceRequest](../../models/operations/pausetriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.PauseTriggerInstanceSecurity](../../models/operations/pausetriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TriggerInstance](../../models/components/triggerinstance.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## resume

Resume a trigger instance.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="resumeTriggerInstance" method="post" path="/rpc/triggers.resume" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.resume({
    id: "756c72e5-3b95-490a-86d6-43d7463d0a18",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { triggersResume } from "@gram/client/funcs/triggersResume.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersResume(gram, {
    id: "756c72e5-3b95-490a-86d6-43d7463d0a18",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersResume failed:", res.error);
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
  useResumeTriggerMutation,
} from "@gram/client/react-query/triggersResume.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ResumeTriggerInstanceRequest](../../models/operations/resumetriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ResumeTriggerInstanceSecurity](../../models/operations/resumetriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TriggerInstance](../../models/components/triggerinstance.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## update

Update a trigger instance.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateTriggerInstance" method="post" path="/rpc/triggers.update" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.triggers.update({
    updateTriggerInstanceForm: {
      id: "c866cdf7-7e93-4846-ae9c-3e59c536fdcc",
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
import { triggersUpdate } from "@gram/client/funcs/triggersUpdate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await triggersUpdate(gram, {
    updateTriggerInstanceForm: {
      id: "c866cdf7-7e93-4846-ae9c-3e59c536fdcc",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("triggersUpdate failed:", res.error);
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
  useUpdateTriggerMutation,
} from "@gram/client/react-query/triggersUpdate.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateTriggerInstanceRequest](../../models/operations/updatetriggerinstancerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateTriggerInstanceSecurity](../../models/operations/updatetriggerinstancesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TriggerInstance](../../models/components/triggerinstance.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
