# Auth
(*auth*)

## Overview

Managed auth for gram producers and dashboard.

### Available Operations

* [authNumberCallback](#authnumbercallback) - callback auth
* [authNumberInfo](#authnumberinfo) - info auth
* [authNumberLogout](#authnumberlogout) - logout auth
* [authNumberSwitchScopes](#authnumberswitchscopes) - switchScopes auth

## authNumberCallback

Handles the authentication callback.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.auth.authNumberCallback({
    sharedToken: "Occaecati autem est itaque in veniam.",
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
import { authAuthNumberCallback } from "@gram/sdk/funcs/authAuthNumberCallback.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await authAuthNumberCallback(gram, {
    sharedToken: "Occaecati autem est itaque in veniam.",
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
| `request`                                                                                                                                                                      | [operations.AuthNumberCallbackRequest](../../models/operations/authnumbercallbackrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.AuthNumberCallbackResponse](../../models/operations/authnumbercallbackresponse.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## authNumberInfo

Provides information about the current authentication status.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.auth.authNumberInfo({
    gramSession: "Consequuntur error suscipit optio sunt eum.",
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
import { authAuthNumberInfo } from "@gram/sdk/funcs/authAuthNumberInfo.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await authAuthNumberInfo(gram, {
    gramSession: "Consequuntur error suscipit optio sunt eum.",
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
  useSessionInfo,
  useSessionInfoSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchSessionInfo,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateSessionInfo,
  invalidateAllSessionInfo,
} from "@gram/sdk/react-query/authAuthNumberInfo.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AuthNumberInfoRequest](../../models/operations/authnumberinforequest.md)                                                                                           | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.AuthNumberInfoResponse](../../models/operations/authnumberinforesponse.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## authNumberLogout

Logs out the current user by clearing their session.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.auth.authNumberLogout({
    gramSession: "Quibusdam quia ea consequuntur.",
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
import { authAuthNumberLogout } from "@gram/sdk/funcs/authAuthNumberLogout.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await authAuthNumberLogout(gram, {
    gramSession: "Quibusdam quia ea consequuntur.",
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
  useLogout,
  useLogoutSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchLogout,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateLogout,
  invalidateAllLogout,
} from "@gram/sdk/react-query/authAuthNumberLogout.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AuthNumberLogoutRequest](../../models/operations/authnumberlogoutrequest.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.AuthNumberLogoutResponse](../../models/operations/authnumberlogoutresponse.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## authNumberSwitchScopes

Switches the authentication scope to a different organization.

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.auth.authNumberSwitchScopes({
    organizationId: "Beatae in dolor aut illo.",
    projectId: "Incidunt natus exercitationem est.",
    gramSession: "Eveniet numquam sint quam.",
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
import { authAuthNumberSwitchScopes } from "@gram/sdk/funcs/authAuthNumberSwitchScopes.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await authAuthNumberSwitchScopes(gram, {
    organizationId: "Beatae in dolor aut illo.",
    projectId: "Incidunt natus exercitationem est.",
    gramSession: "Eveniet numquam sint quam.",
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
  useSwitchScopesMutation
} from "@gram/sdk/react-query/authAuthNumberSwitchScopes.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AuthNumberSwitchScopesRequest](../../models/operations/authnumberswitchscopesrequest.md)                                                                           | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.AuthNumberSwitchScopesResponse](../../models/operations/authnumberswitchscopesresponse.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |