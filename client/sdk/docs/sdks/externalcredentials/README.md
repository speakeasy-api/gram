# ExternalCredentials

## Overview

Manage organization-level external credentials — how Gram authenticates into a customer's AWS or GCP account.

### Available Operations

- [createAwsIam](#createawsiam) - createAwsIamCredential externalCredentials
- [createGcpIam](#creategcpiam) - createGcpIamCredential externalCredentials
- [deleteAwsIam](#deleteawsiam) - deleteAwsIamCredential externalCredentials
- [deleteGcpIam](#deletegcpiam) - deleteGcpIamCredential externalCredentials
- [getAwsIam](#getawsiam) - getAwsIamCredential externalCredentials
- [getGcpIam](#getgcpiam) - getGcpIamCredential externalCredentials
- [list](#list) - listExternalCredentials externalCredentials
- [listAwsIam](#listawsiam) - listAwsIamCredentials externalCredentials
- [listGcpIam](#listgcpiam) - listGcpIamCredentials externalCredentials
- [updateAwsIam](#updateawsiam) - updateAwsIamCredential externalCredentials
- [updateGcpIam](#updategcpiam) - updateGcpIamCredential externalCredentials

## createAwsIam

Create an AWS IAM external credential. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createAwsIamCredential" method="post" path="/rpc/externalCredentials.createAwsIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.createAwsIam({
    createAwsIamCredentialForm: {
      name: "<value>",
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
import { externalCredentialsCreateAwsIam } from "@gram/client/funcs/externalCredentialsCreateAwsIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsCreateAwsIam(gram, {
    createAwsIamCredentialForm: {
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsCreateAwsIam failed:", res.error);
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
  useCreateAwsIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsCreateAwsIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateAwsIamCredentialRequest](../../models/operations/createawsiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateAwsIamCredentialSecurity](../../models/operations/createawsiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AwsIamCredential](../../models/components/awsiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## createGcpIam

Create a GCP IAM external credential. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createGcpIamCredential" method="post" path="/rpc/externalCredentials.createGcpIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.createGcpIam({
    createGcpIamCredentialForm: {
      name: "<value>",
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
import { externalCredentialsCreateGcpIam } from "@gram/client/funcs/externalCredentialsCreateGcpIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsCreateGcpIam(gram, {
    createGcpIamCredentialForm: {
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsCreateGcpIam failed:", res.error);
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
  useCreateGcpIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsCreateGcpIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateGcpIamCredentialRequest](../../models/operations/creategcpiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateGcpIamCredentialSecurity](../../models/operations/creategcpiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GcpIamCredential](../../models/components/gcpiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deleteAwsIam

Soft-delete an AWS IAM external credential by ID. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteAwsIamCredential" method="delete" path="/rpc/externalCredentials.deleteAwsIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.externalCredentials.deleteAwsIam({
    id: "6c2a8c2b-e706-472f-9355-b7b67087c91b",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsDeleteAwsIam } from "@gram/client/funcs/externalCredentialsDeleteAwsIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsDeleteAwsIam(gram, {
    id: "6c2a8c2b-e706-472f-9355-b7b67087c91b",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("externalCredentialsDeleteAwsIam failed:", res.error);
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
  useDeleteAwsIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsDeleteAwsIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteAwsIamCredentialRequest](../../models/operations/deleteawsiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteAwsIamCredentialSecurity](../../models/operations/deleteawsiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deleteGcpIam

Soft-delete a GCP IAM external credential by ID. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteGcpIamCredential" method="delete" path="/rpc/externalCredentials.deleteGcpIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.externalCredentials.deleteGcpIam({
    id: "607b66f2-7b5c-47e2-972d-6f2698416892",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsDeleteGcpIam } from "@gram/client/funcs/externalCredentialsDeleteGcpIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsDeleteGcpIam(gram, {
    id: "607b66f2-7b5c-47e2-972d-6f2698416892",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("externalCredentialsDeleteGcpIam failed:", res.error);
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
  useDeleteGcpIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsDeleteGcpIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteGcpIamCredentialRequest](../../models/operations/deletegcpiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteGcpIamCredentialSecurity](../../models/operations/deletegcpiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getAwsIam

Get an AWS IAM external credential by ID. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getAwsIamCredential" method="get" path="/rpc/externalCredentials.getAwsIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.getAwsIam({
    id: "c8d50c03-25cb-4754-aad4-04ce87dd3c34",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsGetAwsIam } from "@gram/client/funcs/externalCredentialsGetAwsIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsGetAwsIam(gram, {
    id: "c8d50c03-25cb-4754-aad4-04ce87dd3c34",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsGetAwsIam failed:", res.error);
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
  useGetAwsIamCredential,
  useGetAwsIamCredentialSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetAwsIamCredential,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetAwsIamCredential,
  invalidateAllGetAwsIamCredential,
} from "@gram/client/react-query/externalCredentialsGetAwsIam.js";
```

### Parameters

| Parameter              | Type                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetAwsIamCredentialRequest](../../models/operations/getawsiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetAwsIamCredentialSecurity](../../models/operations/getawsiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AwsIamCredential](../../models/components/awsiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getGcpIam

Get a GCP IAM external credential by ID. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getGcpIamCredential" method="get" path="/rpc/externalCredentials.getGcpIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.getGcpIam({
    id: "4879511d-7362-4328-a266-f947689f6c43",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsGetGcpIam } from "@gram/client/funcs/externalCredentialsGetGcpIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsGetGcpIam(gram, {
    id: "4879511d-7362-4328-a266-f947689f6c43",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsGetGcpIam failed:", res.error);
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
  useGetGcpIamCredential,
  useGetGcpIamCredentialSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetGcpIamCredential,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetGcpIamCredential,
  invalidateAllGetGcpIamCredential,
} from "@gram/client/react-query/externalCredentialsGetGcpIam.js";
```

### Parameters

| Parameter              | Type                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetGcpIamCredentialRequest](../../models/operations/getgcpiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetGcpIamCredentialSecurity](../../models/operations/getgcpiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GcpIamCredential](../../models/components/gcpiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listExternalCredentials" method="get" path="/rpc/externalCredentials.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.list();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsList } from "@gram/client/funcs/externalCredentialsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsList(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsList failed:", res.error);
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
  useListExternalCredentials,
  useListExternalCredentialsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListExternalCredentials,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListExternalCredentials,
  invalidateAllListExternalCredentials,
} from "@gram/client/react-query/externalCredentialsList.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListExternalCredentialsRequest](../../models/operations/listexternalcredentialsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListExternalCredentialsSecurity](../../models/operations/listexternalcredentialssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListExternalCredentialsResult](../../models/components/listexternalcredentialsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listAwsIam

List the organization's AWS IAM external credentials. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listAwsIamCredentials" method="get" path="/rpc/externalCredentials.listAwsIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.listAwsIam();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsListAwsIam } from "@gram/client/funcs/externalCredentialsListAwsIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsListAwsIam(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsListAwsIam failed:", res.error);
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
  useListAwsIamCredentials,
  useListAwsIamCredentialsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListAwsIamCredentials,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListAwsIamCredentials,
  invalidateAllListAwsIamCredentials,
} from "@gram/client/react-query/externalCredentialsListAwsIam.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListAwsIamCredentialsRequest](../../models/operations/listawsiamcredentialsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListAwsIamCredentialsSecurity](../../models/operations/listawsiamcredentialssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListExternalCredentialsResult](../../models/components/listexternalcredentialsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listGcpIam

List the organization's GCP IAM external credentials. Requires org:read.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listGcpIamCredentials" method="get" path="/rpc/externalCredentials.listGcpIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.listGcpIam();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { externalCredentialsListGcpIam } from "@gram/client/funcs/externalCredentialsListGcpIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsListGcpIam(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsListGcpIam failed:", res.error);
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
  useListGcpIamCredentials,
  useListGcpIamCredentialsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListGcpIamCredentials,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListGcpIamCredentials,
  invalidateAllListGcpIamCredentials,
} from "@gram/client/react-query/externalCredentialsListGcpIam.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListGcpIamCredentialsRequest](../../models/operations/listgcpiamcredentialsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListGcpIamCredentialsSecurity](../../models/operations/listgcpiamcredentialssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListExternalCredentialsResult](../../models/components/listexternalcredentialsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateAwsIam

Replace an AWS IAM external credential's configuration. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateAwsIamCredential" method="post" path="/rpc/externalCredentials.updateAwsIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.updateAwsIam({
    updateAwsIamCredentialRequestBody: {
      id: "fb8512f4-107a-433e-93bd-ea7524b81f8d",
      name: "<value>",
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
import { externalCredentialsUpdateAwsIam } from "@gram/client/funcs/externalCredentialsUpdateAwsIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsUpdateAwsIam(gram, {
    updateAwsIamCredentialRequestBody: {
      id: "fb8512f4-107a-433e-93bd-ea7524b81f8d",
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsUpdateAwsIam failed:", res.error);
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
  useUpdateAwsIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsUpdateAwsIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateAwsIamCredentialRequest](../../models/operations/updateawsiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateAwsIamCredentialSecurity](../../models/operations/updateawsiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AwsIamCredential](../../models/components/awsiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateGcpIam

Replace a GCP IAM external credential's configuration. Requires org:admin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateGcpIamCredential" method="post" path="/rpc/externalCredentials.updateGcpIam" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.externalCredentials.updateGcpIam({
    updateGcpIamCredentialRequestBody: {
      id: "986a9d5e-aa48-4fd4-bdf9-40b4fad65e9d",
      name: "<value>",
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
import { externalCredentialsUpdateGcpIam } from "@gram/client/funcs/externalCredentialsUpdateGcpIam.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await externalCredentialsUpdateGcpIam(gram, {
    updateGcpIamCredentialRequestBody: {
      id: "986a9d5e-aa48-4fd4-bdf9-40b4fad65e9d",
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("externalCredentialsUpdateGcpIam failed:", res.error);
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
  useUpdateGcpIamCredentialMutation,
} from "@gram/client/react-query/externalCredentialsUpdateGcpIam.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateGcpIamCredentialRequest](../../models/operations/updategcpiamcredentialrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateGcpIamCredentialSecurity](../../models/operations/updategcpiamcredentialsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GcpIamCredential](../../models/components/gcpiamcredential.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
