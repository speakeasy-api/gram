# System
(*system*)

## Overview

Exposes service health and status information.

### Available Operations

* [systemNumberHealthCheck](#systemnumberhealthcheck) - healthCheck system

## systemNumberHealthCheck

Check the health of the service.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.system.systemNumberHealthCheck();

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { systemSystemNumberHealthCheck } from "@gram/sdk/funcs/systemSystemNumberHealthCheck.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await systemSystemNumberHealthCheck(gram);

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
  useSystemSystemNumberHealthCheck,
  useSystemSystemNumberHealthCheckSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchSystemSystemNumberHealthCheck,
  
  // Utility to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateAllSystemSystemNumberHealthCheck,
} from "@gram/sdk/react-query/systemSystemNumberHealthCheck.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.HealthCheckResult](../../models/components/healthcheckresult.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |