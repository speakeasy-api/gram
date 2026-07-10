# AiIntegrations

## Overview

Manage organization-level AI provider integrations.

### Available Operations

* [deleteConfig](#deleteconfig) - deleteConfig aiIntegrations
* [getConfig](#getconfig) - getConfig aiIntegrations
* [upsertConfig](#upsertconfig) - upsertConfig aiIntegrations

## deleteConfig

Delete the org-wide AI integration config for a provider.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteAIIntegrationConfig" method="post" path="/rpc/aiIntegrations.deleteConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.aiIntegrations.deleteConfig({
    deleteConfigRequestBody: {
      provider: "<value>",
    },
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { aiIntegrationsDeleteConfig } from "@gram/client/funcs/aiIntegrationsDeleteConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await aiIntegrationsDeleteConfig(gram, {
    deleteConfigRequestBody: {
      provider: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    
  } else {
    console.log("aiIntegrationsDeleteConfig failed:", res.error);
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
  useDeleteAIIntegrationConfigMutation
} from "@gram/client/react-query/aiIntegrationsDeleteConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteAIIntegrationConfigRequest](../../models/operations/deleteaiintegrationconfigrequest.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.DeleteAIIntegrationConfigSecurity](../../models/operations/deleteaiintegrationconfigsecurity.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
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

Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getAIIntegrationConfig" method="get" path="/rpc/aiIntegrations.getConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.aiIntegrations.getConfig({
    provider: "<value>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { aiIntegrationsGetConfig } from "@gram/client/funcs/aiIntegrationsGetConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await aiIntegrationsGetConfig(gram, {
    provider: "<value>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("aiIntegrationsGetConfig failed:", res.error);
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
  useAiIntegrationConfig,
  useAiIntegrationConfigSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchAiIntegrationConfig,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateAiIntegrationConfig,
  invalidateAllAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationsGetConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetAIIntegrationConfigRequest](../../models/operations/getaiintegrationconfigrequest.md)                                                                           | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetAIIntegrationConfigSecurity](../../models/operations/getaiintegrationconfigsecurity.md)                                                                         | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AIIntegrationConfig](../../models/components/aiintegrationconfig.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## upsertConfig

Create or update the org-wide AI integration config for a provider.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="upsertAIIntegrationConfig" method="post" path="/rpc/aiIntegrations.upsertConfig" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.aiIntegrations.upsertConfig({
    upsertConfigRequestBody: {
      apiKey: "<value>",
      enabled: false,
      provider: "<value>",
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
import { aiIntegrationsUpsertConfig } from "@gram/client/funcs/aiIntegrationsUpsertConfig.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await aiIntegrationsUpsertConfig(gram, {
    upsertConfigRequestBody: {
      apiKey: "<value>",
      enabled: false,
      provider: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("aiIntegrationsUpsertConfig failed:", res.error);
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
  useUpsertAIIntegrationConfigMutation
} from "@gram/client/react-query/aiIntegrationsUpsertConfig.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.UpsertAIIntegrationConfigRequest](../../models/operations/upsertaiintegrationconfigrequest.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.UpsertAIIntegrationConfigSecurity](../../models/operations/upsertaiintegrationconfigsecurity.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AIIntegrationConfig](../../models/components/aiintegrationconfig.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |