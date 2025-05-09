# Environments
(*environments*)

## Overview

Managing toolset environments.

### Available Operations

* [create](#create) - createEnvironment environments
* [deleteBySlug](#deletebyslug) - deleteEnvironment environments
* [list](#list) - listEnvironments environments
* [updateBySlug](#updatebyslug) - updateEnvironment environments

## create

Create a new environment

### Example Usage

```typescript
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const result = await gram.environments.create({
    createEnvironmentForm: {
      entries: [
        {
          name: "<value>",
          value: "<value>",
        },
      ],
      name: "<value>",
      organizationId: "<id>",
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
import { GramCore } from "@gram/client/core.js";
import { environmentsCreate } from "@gram/client/funcs/environmentsCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const res = await environmentsCreate(gram, {
    createEnvironmentForm: {
      entries: [
        {
          name: "<value>",
          value: "<value>",
        },
      ],
      name: "<value>",
      organizationId: "<id>",
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
  useCreateEnvironmentMutation
} from "@gram/client/react-query/environmentsCreate.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.CreateEnvironmentRequest](../../models/operations/createenvironmentrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Environment](../../models/components/environment.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## deleteBySlug

Delete an environment

### Example Usage

```typescript
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  await gram.environments.deleteBySlug({
    slug: "<value>",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { environmentsDeleteBySlug } from "@gram/client/funcs/environmentsDeleteBySlug.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const res = await environmentsDeleteBySlug(gram, {
    slug: "<value>",
  });

  if (!res.ok) {
    throw res.error;
  }

  const { value: result } = res;

  
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
  useDeleteEnvironmentMutation
} from "@gram/client/react-query/environmentsDeleteBySlug.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.DeleteEnvironmentRequest](../../models/operations/deleteenvironmentrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## list

List all environments for an organization

### Example Usage

```typescript
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const result = await gram.environments.list();

  // Handle the result
  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { environmentsList } from "@gram/client/funcs/environmentsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const res = await environmentsList(gram);

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
  useListEnvironments,
  useListEnvironmentsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListEnvironments,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListEnvironments,
  invalidateAllListEnvironments,
} from "@gram/client/react-query/environmentsList.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListEnvironmentsRequest](../../models/operations/listenvironmentsrequest.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListEnvironmentsResult](../../models/components/listenvironmentsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## updateBySlug

Update an environment

### Example Usage

```typescript
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const result = await gram.environments.updateBySlug({
    slug: "<value>",
    updateEnvironmentRequestBody: {
      entriesToRemove: [],
      entriesToUpdate: [],
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
import { GramCore } from "@gram/client/core.js";
import { environmentsUpdateBySlug } from "@gram/client/funcs/environmentsUpdateBySlug.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  security: {
    projectSlugHeaderGramProject: process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
  },
});

async function run() {
  const res = await environmentsUpdateBySlug(gram, {
    slug: "<value>",
    updateEnvironmentRequestBody: {
      entriesToRemove: [],
      entriesToUpdate: [],
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
  useUpdateEnvironmentMutation
} from "@gram/client/react-query/environmentsUpdateBySlug.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.UpdateEnvironmentRequest](../../models/operations/updateenvironmentrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Environment](../../models/components/environment.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |