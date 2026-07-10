# OtelForwarding

## Overview

Manage per-organization forwarding of inbound OTEL hook payloads to a customer-owned endpoint.

### Available Operations

* [deleteConfig](#deleteconfig) - deleteConfig otelForwarding
* [getConfig](#getconfig) - getConfig otelForwarding
* [upsertConfig](#upsertconfig) - upsertConfig otelForwarding

## deleteConfig

Delete the org-wide OTEL forwarding config.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteOtelForwardingConfig" method="post" path="/rpc/otelForwarding.deleteConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.otelForwarding.deleteConfig();


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { otelForwardingDeleteConfig } from "@gram/client/funcs/otelForwardingDeleteConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await otelForwardingDeleteConfig(gram);
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("otelForwardingDeleteConfig failed:", res.error);
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
  useDeleteOtelForwardingConfigMutation
} from "@gram/client/react-query/otelForwardingDeleteConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteOtelForwardingConfigRequest](../../models/operations/deleteotelforwardingconfigrequest.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.DeleteOtelForwardingConfigSecurity](../../models/operations/deleteotelforwardingconfigsecurity.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
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

## getConfig

Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getOtelForwardingConfig" method="get" path="/rpc/otelForwarding.getConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.otelForwarding.getConfig();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { otelForwardingGetConfig } from "@gram/client/funcs/otelForwardingGetConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await otelForwardingGetConfig(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("otelForwardingGetConfig failed:", res.error);
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
  useOtelForwardingConfig,
  useOtelForwardingConfigSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchOtelForwardingConfig,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateOtelForwardingConfig,
  invalidateAllOtelForwardingConfig,
} from "@gram/client/react-query/otelForwardingGetConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetOtelForwardingConfigRequest](../../models/operations/getotelforwardingconfigrequest.md)                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetOtelForwardingConfigSecurity](../../models/operations/getotelforwardingconfigsecurity.md)                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.OtelForwardingConfig](../../models/components/otelforwardingconfig.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## upsertConfig

Create or update the org-wide OTEL forwarding config. Replaces the full header set on each call.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="upsertOtelForwardingConfig" method="post" path="/rpc/otelForwarding.upsertConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.otelForwarding.upsertConfig({
    upsertConfigRequestBody2: {
      enabled: true,
      endpointUrl: "https://damp-armchair.name/",
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
import { otelForwardingUpsertConfig } from "@gram/client/funcs/otelForwardingUpsertConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await otelForwardingUpsertConfig(gram, {
    upsertConfigRequestBody2: {
      enabled: true,
      endpointUrl: "https://damp-armchair.name/",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("otelForwardingUpsertConfig failed:", res.error);
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
  useUpsertOtelForwardingConfigMutation
} from "@gram/client/react-query/otelForwardingUpsertConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.UpsertOtelForwardingConfigRequest](../../models/operations/upsertotelforwardingconfigrequest.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.UpsertOtelForwardingConfigSecurity](../../models/operations/upsertotelforwardingconfigsecurity.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.OtelForwardingConfig](../../models/components/otelforwardingconfig.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |