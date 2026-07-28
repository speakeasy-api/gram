# HooksServerNames

## Overview

Manages display name overrides for hooks servers.

### Available Operations

- [deleteServerNameOverride](#deleteservernameoverride) - delete hooksServerNames
- [listServerNameOverrides](#listservernameoverrides) - list hooksServerNames
- [upsertServerNameOverride](#upsertservernameoverride) - upsert hooksServerNames

## deleteServerNameOverride

Delete a server name display override

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteServerNameOverride" method="post" path="/rpc/hooks.deleteServerNameOverride" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.hooksServerNames.deleteServerNameOverride({
    deleteRequestBody: {
      overrideId: "<id>",
    },
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { hooksServerNamesDeleteServerNameOverride } from "@gram/client/funcs/hooksServerNamesDeleteServerNameOverride.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await hooksServerNamesDeleteServerNameOverride(gram, {
    deleteRequestBody: {
      overrideId: "<id>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("hooksServerNamesDeleteServerNameOverride failed:", res.error);
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
  useHooksServerNamesDeleteServerNameOverrideMutation,
} from "@gram/client/react-query/hooksServerNamesDeleteServerNameOverride.js";
```

### Parameters

| Parameter              | Type                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteServerNameOverrideRequest](../../models/operations/deleteservernameoverriderequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteServerNameOverrideSecurity](../../models/operations/deleteservernameoverridesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listServerNameOverrides

List all server name display overrides for a project

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listServerNameOverrides" method="get" path="/rpc/hooks.listServerNameOverrides" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.hooksServerNames.listServerNameOverrides();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { hooksServerNamesListServerNameOverrides } from "@gram/client/funcs/hooksServerNamesListServerNameOverrides.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await hooksServerNamesListServerNameOverrides(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("hooksServerNamesListServerNameOverrides failed:", res.error);
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
  useHooksServerNamesListServerNameOverrides,
  useHooksServerNamesListServerNameOverridesSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchHooksServerNamesListServerNameOverrides,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateHooksServerNamesListServerNameOverrides,
  invalidateAllHooksServerNamesListServerNameOverrides,
} from "@gram/client/react-query/hooksServerNamesListServerNameOverrides.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListServerNameOverridesRequest](../../models/operations/listservernameoverridesrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListServerNameOverridesSecurity](../../models/operations/listservernameoverridessecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ServerNameOverride[]](../../models/.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## upsertServerNameOverride

Create or update a server name display override

### Example Usage

<!-- UsageSnippet language="typescript" operationID="upsertServerNameOverride" method="post" path="/rpc/hooks.upsertServerNameOverride" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.hooksServerNames.upsertServerNameOverride({
    upsertRequestBody: {
      displayName: "Jerad.Schiller10",
      rawServerName: "<value>",
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
import { hooksServerNamesUpsertServerNameOverride } from "@gram/client/funcs/hooksServerNamesUpsertServerNameOverride.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await hooksServerNamesUpsertServerNameOverride(gram, {
    upsertRequestBody: {
      displayName: "Jerad.Schiller10",
      rawServerName: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("hooksServerNamesUpsertServerNameOverride failed:", res.error);
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
  useHooksServerNamesUpsertServerNameOverrideMutation,
} from "@gram/client/react-query/hooksServerNamesUpsertServerNameOverride.js";
```

### Parameters

| Parameter              | Type                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpsertServerNameOverrideRequest](../../models/operations/upsertservernameoverriderequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpsertServerNameOverrideSecurity](../../models/operations/upsertservernameoverridesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ServerNameOverride](../../models/components/servernameoverride.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
