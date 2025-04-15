# Toolsets
(*toolsets*)

## Overview

Managed toolsets for gram AI consumers.

### Available Operations

* [create](#create) - createToolset toolsets
* [deleteBySlug](#deletebyslug) - deleteToolset toolsets
* [getById](#getbyid) - getToolsetDetails toolsets
* [list](#list) - listToolsets toolsets
* [updateBySlug](#updatebyslug) - updateToolset toolsets

## create

Create a new toolset with associated tools

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await gramAPI.toolsets.create({
    createToolsetRequestBody: {
      name: "<value>",
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
import { GramAPICore } from "@gram/sdk/core.js";
import { toolsetsCreate } from "@gram/sdk/funcs/toolsetsCreate.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await toolsetsCreate(gramAPI, {
    createToolsetRequestBody: {
      name: "<value>",
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

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberCreateToolsetRequest](../../models/operations/toolsetsnumbercreatetoolsetrequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ToolsetDetails](../../models/components/toolsetdetails.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## deleteBySlug

Delete a toolset by its ID

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  await gramAPI.toolsets.deleteBySlug({
    slug: "<value>",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { toolsetsDeleteBySlug } from "@gram/sdk/funcs/toolsetsDeleteBySlug.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await toolsetsDeleteBySlug(gramAPI, {
    slug: "<value>",
  });

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberDeleteToolsetRequest](../../models/operations/toolsetsnumberdeletetoolsetrequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## getById

Get detailed information about a toolset including full HTTP tool definitions

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await gramAPI.toolsets.getById({
    slug: "<value>",
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { toolsetsGetById } from "@gram/sdk/funcs/toolsetsGetById.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await toolsetsGetById(gramAPI, {
    slug: "<value>",
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

## list

List all toolsets for a project

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await gramAPI.toolsets.list();

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { toolsetsList } from "@gram/sdk/funcs/toolsetsList.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await toolsetsList(gramAPI);

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  // Handle the result
  console.log(result);
}

run();
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

## updateBySlug

Update a toolset's properties including name, description, and HTTP tools

### Example Usage

```typescript
import { GramAPI } from "@gram/sdk";

const gramAPI = new GramAPI({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const result = await gramAPI.toolsets.updateBySlug({
    slug: "<value>",
    updateToolsetRequestBody: {},
  });

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramAPICore } from "@gram/sdk/core.js";
import { toolsetsUpdateBySlug } from "@gram/sdk/funcs/toolsetsUpdateBySlug.js";

// Use `GramAPICore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gramAPI = new GramAPICore({
  security: {
    projectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
    sessionHeaderGramSession: "<YOUR_API_KEY_HERE>",
  },
});

async function run() {
  const res = await toolsetsUpdateBySlug(gramAPI, {
    slug: "<value>",
    updateToolsetRequestBody: {},
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

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ToolsetsNumberUpdateToolsetRequest](../../models/operations/toolsetsnumberupdatetoolsetrequest.md)                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ToolsetDetails](../../models/components/toolsetdetails.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |