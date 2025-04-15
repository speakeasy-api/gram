# Instances
(*instances*)

## Overview

Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.

### Available Operations

* [instancesNumberLoadInstance](#instancesnumberloadinstance) - loadInstance instances

## instancesNumberLoadInstance

load all relevant data for an instance of a toolset and environment

### Example Usage

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.instances.instancesNumberLoadInstance({
    option1: {
      projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
      sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
    },
  }, {
    toolsetSlug: "<value>",
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { instancesInstancesNumberLoadInstance } from "@gram/client/funcs/instancesInstancesNumberLoadInstance.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await instancesInstancesNumberLoadInstance(gram, {
    option1: {
      projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
      sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
    },
  }, {
    toolsetSlug: "<value>",
  });

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  // Handle the result
  console.log(result);
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
  useLoadInstance,
  useLoadInstanceSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchLoadInstance,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateLoadInstance,
  invalidateAllLoadInstance,
} from "@gram/client/react-query/instancesInstancesNumberLoadInstance.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.InstancesNumberLoadInstanceRequest](../../models/operations/instancesnumberloadinstancerequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.InstancesNumberLoadInstanceSecurity](../../models/operations/instancesnumberloadinstancesecurity.md)                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.InstanceResult](../../models/components/instanceresult.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |