# Keys
(*keys*)

## Overview

Managing system api keys.

### Available Operations

* [keysNumberCreateKey](#keysnumbercreatekey) - createKey keys
* [keysNumberListKeys](#keysnumberlistkeys) - listKeys keys
* [keysNumberRevokeKey](#keysnumberrevokekey) - revokeKey keys

## keysNumberCreateKey

Create a new api key

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.keys.keysNumberCreateKey({
    gramSession: "Esse est omnis fuga illum expedita corporis.",
    createKeyForm: {
      name: "At expedita magni perferendis amet unde.",
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
import { keysKeysNumberCreateKey } from "@gram/sdk/funcs/keysKeysNumberCreateKey.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await keysKeysNumberCreateKey(gram, {
    gramSession: "Esse est omnis fuga illum expedita corporis.",
    createKeyForm: {
      name: "At expedita magni perferendis amet unde.",
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
  useCreateAPIKeyMutation
} from "@gram/sdk/react-query/keysKeysNumberCreateKey.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.KeysNumberCreateKeyRequest](../../models/operations/keysnumbercreatekeyrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Key](../../models/components/key.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## keysNumberListKeys

List all api keys for an organization

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const result = await gram.keys.keysNumberListKeys({
    gramSession: "Vitae sed doloremque et perspiciatis.",
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
import { keysKeysNumberListKeys } from "@gram/sdk/funcs/keysKeysNumberListKeys.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await keysKeysNumberListKeys(gram, {
    gramSession: "Vitae sed doloremque et perspiciatis.",
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
  useListAPIKeys,
  useListAPIKeysSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListAPIKeys,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListAPIKeys,
  invalidateAllListAPIKeys,
} from "@gram/sdk/react-query/keysKeysNumberListKeys.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.KeysNumberListKeysRequest](../../models/operations/keysnumberlistkeysrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListKeysResult](../../models/components/listkeysresult.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## keysNumberRevokeKey

Revoke a api key

### Example Usage

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  await gram.keys.keysNumberRevokeKey({
    id: "Enim accusantium nisi est enim.",
    gramSession: "Itaque facilis.",
  });


}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/sdk/core.js";
import { keysKeysNumberRevokeKey } from "@gram/sdk/funcs/keysKeysNumberRevokeKey.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore({
  sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"] ?? "",
});

async function run() {
  const res = await keysKeysNumberRevokeKey(gram, {
    id: "Enim accusantium nisi est enim.",
    gramSession: "Itaque facilis.",
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
  useRevokeAPIKeysMutation
} from "@gram/sdk/react-query/keysKeysNumberRevokeKey.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.KeysNumberRevokeKeyRequest](../../models/operations/keysnumberrevokekeyrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |