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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberCreateToolset({
    gramSession: "Quis numquam exercitationem earum vel eveniet culpa.",
    gramProject: "Nam dolorem ipsum.",
    createToolsetRequestBody: {
      description: "Blanditiis qui mollitia molestias iste mollitia consequatur.",
      httpToolIds: [
        "Ipsa minus quo nihil.",
        "Et veritatis totam.",
      ],
      name: "Optio est.",
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberCreateToolset(gram, {
    gramSession: "Quis numquam exercitationem earum vel eveniet culpa.",
    gramProject: "Nam dolorem ipsum.",
    createToolsetRequestBody: {
      description: "Blanditiis qui mollitia molestias iste mollitia consequatur.",
      httpToolIds: [
        "Ipsa minus quo nihil.",
        "Et veritatis totam.",
      ],
      name: "Optio est.",
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
  useCreateToolsetMutation
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberGetToolsetDetails({
    id: "Dolor veniam quae sed labore et.",
    gramSession: "Rerum rem ducimus.",
    gramProject: "Est molestiae omnis ducimus ut et delectus.",
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberGetToolsetDetails(gram, {
    id: "Dolor veniam quae sed labore et.",
    gramSession: "Rerum rem ducimus.",
    gramProject: "Est molestiae omnis ducimus ut et delectus.",
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
  useToolset,
  useToolsetSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchToolset,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateToolset,
  invalidateAllToolset,
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberListToolsets({
    gramSession: "Alias totam non aliquam maxime.",
    gramProject: "Consequatur recusandae non tenetur rem.",
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberListToolsets(gram, {
    gramSession: "Alias totam non aliquam maxime.",
    gramProject: "Consequatur recusandae non tenetur rem.",
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
  useListToolsets,
  useListToolsetsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListToolsets,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListToolsets,
  invalidateAllListToolsets,
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.toolsets.toolsetsNumberUpdateToolset({
    id: "Voluptatem alias in suscipit voluptates unde similique.",
    gramSession: "Omnis reprehenderit.",
    gramProject: "Veritatis quaerat sit et.",
    updateToolsetRequestBody: {
      description: "Iste optio ullam.",
      httpToolIdsToAdd: [
        "Animi tenetur nesciunt et est et.",
        "Nam quis.",
      ],
      httpToolIdsToRemove: [
        "Animi sequi.",
        "Amet sapiente.",
      ],
      name: "Soluta fuga.",
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
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await toolsetsToolsetsNumberUpdateToolset(gram, {
    id: "Voluptatem alias in suscipit voluptates unde similique.",
    gramSession: "Omnis reprehenderit.",
    gramProject: "Veritatis quaerat sit et.",
    updateToolsetRequestBody: {
      description: "Iste optio ullam.",
      httpToolIdsToAdd: [
        "Animi tenetur nesciunt et est et.",
        "Nam quis.",
      ],
      httpToolIdsToRemove: [
        "Animi sequi.",
        "Amet sapiente.",
      ],
      name: "Soluta fuga.",
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
  useUpdateToolsetMutation
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