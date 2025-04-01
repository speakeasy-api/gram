# Toolsets
(*toolsets*)

## Overview

Managed toolsets for gram AI consumers.

### Available Operations

* [toolsetsNumberCreateToolset](#toolsetsnumbercreatetoolset) - createToolset toolsets
* [toolsetsNumberGetToolsetDetails](#toolsetsnumbergettoolsetdetails) - getToolsetDetails toolsets
* [toolsetsNumberListToolsets](#toolsetsnumberlisttoolsets) - listToolsets toolsets
* [toolsetsNumberUpdateToolset](#toolsetsnumberupdatetoolset) - updateToolset toolsets

## toolsetsNumberCreateToolset

Create a new toolset with associated tools

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberCreateToolset({
    xGramSession: "Earum dolorum ipsam enim temporibus aut omnis.",
    createToolsetForm: {
      description: "Voluptatem mollitia dolor explicabo doloribus.",
      httpToolIds: [
        "Assumenda commodi pariatur reprehenderit.",
        "Ipsa molestiae voluptas nemo.",
        "Iusto voluptas culpa sed.",
      ],
      name: "Labore consectetur doloribus distinctio officiis.",
      projectId: "Eos dolorem excepturi voluptatibus quisquam.",
    },
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { toolsetsToolsetsNumberCreateToolset } from "@gram/sdk/funcs/toolsetsToolsetsNumberCreateToolset.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberCreateToolset(gram, {
    xGramSession: "Earum dolorum ipsam enim temporibus aut omnis.",
    createToolsetForm: {
      description: "Voluptatem mollitia dolor explicabo doloribus.",
      httpToolIds: [
        "Assumenda commodi pariatur reprehenderit.",
        "Ipsa molestiae voluptas nemo.",
        "Iusto voluptas culpa sed.",
      ],
      name: "Labore consectetur doloribus distinctio officiis.",
      projectId: "Eos dolorem excepturi voluptatibus quisquam.",
    },
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
  // Mutation hook for triggering the API call.
  useToolsetsToolsetsNumberCreateToolsetMutation
} from "@gram/sdk/react-query/toolsetsToolsetsNumberCreateToolset.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberCreateToolsetRequest](../../models/operations/toolsetsnumbercreatetoolsetrequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Toolset](../../models/components/toolset.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## toolsetsNumberGetToolsetDetails

Get detailed information about a toolset including full HTTP tool definitions

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberGetToolsetDetails({
    id: "Et consectetur velit asperiores.",
    xGramSession: "Optio qui quo.",
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { toolsetsToolsetsNumberGetToolsetDetails } from "@gram/sdk/funcs/toolsetsToolsetsNumberGetToolsetDetails.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberGetToolsetDetails(gram, {
    id: "Et consectetur velit asperiores.",
    xGramSession: "Optio qui quo.",
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
  useToolsetsToolsetsNumberGetToolsetDetails,
  useToolsetsToolsetsNumberGetToolsetDetailsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchToolsetsToolsetsNumberGetToolsetDetails,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateToolsetsToolsetsNumberGetToolsetDetails,
  invalidateAllToolsetsToolsetsNumberGetToolsetDetails,
} from "@gram/sdk/react-query/toolsetsToolsetsNumberGetToolsetDetails.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberGetToolsetDetailsRequest](../../models/operations/toolsetsnumbergettoolsetdetailsrequest.md)                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ToolsetDetails](../../models/components/toolsetdetails.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## toolsetsNumberListToolsets

List all toolsets for a project

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberListToolsets({
    projectId: "Quos neque voluptatum rerum veniam commodi.",
    xGramSession: "Atque fuga.",
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { toolsetsToolsetsNumberListToolsets } from "@gram/sdk/funcs/toolsetsToolsetsNumberListToolsets.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberListToolsets(gram, {
    projectId: "Quos neque voluptatum rerum veniam commodi.",
    xGramSession: "Atque fuga.",
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
  useToolsetsToolsetsNumberListToolsets,
  useToolsetsToolsetsNumberListToolsetsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchToolsetsToolsetsNumberListToolsets,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateToolsetsToolsetsNumberListToolsets,
  invalidateAllToolsetsToolsetsNumberListToolsets,
} from "@gram/sdk/react-query/toolsetsToolsetsNumberListToolsets.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberListToolsetsRequest](../../models/operations/toolsetsnumberlisttoolsetsrequest.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListToolsetsResult](../../models/components/listtoolsetsresult.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## toolsetsNumberUpdateToolset

Update a toolset's properties including name, description, and HTTP tools

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberUpdateToolset({
    id: "Eius debitis esse fugiat architecto.",
    xGramSession: "Aut nam labore quidem.",
    updateToolsetRequestBody: {
      description: "Laboriosam voluptatem ullam doloribus ut quaerat.",
      httpToolIdsToAdd: [
        "Fuga id ea et esse.",
        "Error et enim nostrum doloremque.",
        "Maxime error voluptatum dolore debitis.",
      ],
      httpToolIdsToRemove: [
        "Magnam nostrum aut sunt itaque.",
        "Assumenda ut.",
      ],
      name: "Nemo assumenda quas dolor.",
    },
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { toolsetsToolsetsNumberUpdateToolset } from "@gram/sdk/funcs/toolsetsToolsetsNumberUpdateToolset.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  gramSessionHeaderXGramSession: process.env["GRAM_GRAM_SESSION_HEADER_X_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberUpdateToolset(gram, {
    id: "Eius debitis esse fugiat architecto.",
    xGramSession: "Aut nam labore quidem.",
    updateToolsetRequestBody: {
      description: "Laboriosam voluptatem ullam doloribus ut quaerat.",
      httpToolIdsToAdd: [
        "Fuga id ea et esse.",
        "Error et enim nostrum doloremque.",
        "Maxime error voluptatum dolore debitis.",
      ],
      httpToolIdsToRemove: [
        "Magnam nostrum aut sunt itaque.",
        "Assumenda ut.",
      ],
      name: "Nemo assumenda quas dolor.",
    },
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
  // Mutation hook for triggering the API call.
  useToolsetsToolsetsNumberUpdateToolsetMutation
} from "@gram/sdk/react-query/toolsetsToolsetsNumberUpdateToolset.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberUpdateToolsetRequest](../../models/operations/toolsetsnumberupdatetoolsetrequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Toolset](../../models/components/toolset.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |