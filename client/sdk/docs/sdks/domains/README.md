# Domains

## Overview

Manage custom domains for gram.

### Available Operations

- [deleteDomain](#deletedomain) - deleteDomain domains
- [getDomain](#getdomain) - getDomain domains
- [listMcpEndpoints](#listmcpendpoints) - listMcpEndpoints domains
- [registerDomain](#registerdomain) - createDomain domains
- [updateDomain](#updatedomain) - updateDomain domains

## deleteDomain

Delete a custom domain

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteDomain" method="delete" path="/rpc/domain.delete" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.domains.deleteDomain();
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { domainsDeleteDomain } from "@gram/client/funcs/domainsDeleteDomain.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await domainsDeleteDomain(gram);
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("domainsDeleteDomain failed:", res.error);
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
  useDeleteDomainMutation,
} from "@gram/client/react-query/domainsDeleteDomain.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteDomainRequest](../../models/operations/deletedomainrequest.md)        | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteDomainSecurity](../../models/operations/deletedomainsecurity.md)      | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getDomain

Get the custom domain for an organization

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getDomain" method="get" path="/rpc/domain.get" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.domains.getDomain();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { domainsGetDomain } from "@gram/client/funcs/domainsGetDomain.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await domainsGetDomain(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("domainsGetDomain failed:", res.error);
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
  useGetDomain,
  useGetDomainSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetDomain,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetDomain,
  invalidateAllGetDomain,
} from "@gram/client/react-query/domainsGetDomain.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetDomainRequest](../../models/operations/getdomainrequest.md)              | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetDomainSecurity](../../models/operations/getdomainsecurity.md)            | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.CustomDomain](../../models/components/customdomain.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listMcpEndpoints

List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listCustomDomainMcpEndpoints" method="get" path="/rpc/domain.listMcpEndpoints" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.domains.listMcpEndpoints();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { domainsListMcpEndpoints } from "@gram/client/funcs/domainsListMcpEndpoints.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await domainsListMcpEndpoints(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("domainsListMcpEndpoints failed:", res.error);
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
  useCustomDomainMcpEndpoints,
  useCustomDomainMcpEndpointsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchCustomDomainMcpEndpoints,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateCustomDomainMcpEndpoints,
  invalidateAllCustomDomainMcpEndpoints,
} from "@gram/client/react-query/domainsListMcpEndpoints.js";
```

### Parameters

| Parameter              | Type                                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListCustomDomainMcpEndpointsRequest](../../models/operations/listcustomdomainmcpendpointsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListCustomDomainMcpEndpointsSecurity](../../models/operations/listcustomdomainmcpendpointssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListCustomDomainMcpEndpointsResult](../../models/components/listcustomdomainmcpendpointsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## registerDomain

Create a custom domain for an organization

### Example Usage

<!-- UsageSnippet language="typescript" operationID="registerDomain" method="post" path="/rpc/domain.register" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.domains.registerDomain({
    createDomainRequestBody: {
      domain: "cooperative-partridge.name",
    },
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { domainsRegisterDomain } from "@gram/client/funcs/domainsRegisterDomain.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await domainsRegisterDomain(gram, {
    createDomainRequestBody: {
      domain: "cooperative-partridge.name",
    },
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("domainsRegisterDomain failed:", res.error);
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
  useRegisterDomainMutation,
} from "@gram/client/react-query/domainsRegisterDomain.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RegisterDomainRequest](../../models/operations/registerdomainrequest.md)    | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RegisterDomainSecurity](../../models/operations/registerdomainsecurity.md)  | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateDomain

Update the IP allowlist for the organization's custom domain

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateDomain" method="post" path="/rpc/domain.update" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.domains.updateDomain({
    updateDomainRequestBody: {
      ipAllowlist: ["<value 1>", "<value 2>", "<value 3>"],
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
import { domainsUpdateDomain } from "@gram/client/funcs/domainsUpdateDomain.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await domainsUpdateDomain(gram, {
    updateDomainRequestBody: {
      ipAllowlist: ["<value 1>", "<value 2>", "<value 3>"],
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("domainsUpdateDomain failed:", res.error);
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
  useUpdateDomainMutation,
} from "@gram/client/react-query/domainsUpdateDomain.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateDomainRequest](../../models/operations/updatedomainrequest.md)        | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateDomainSecurity](../../models/operations/updatedomainsecurity.md)      | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.CustomDomain](../../models/components/customdomain.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
