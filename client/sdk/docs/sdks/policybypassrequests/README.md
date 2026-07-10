# Risk.PolicyBypassRequests

## Overview

### Available Operations

- [approve](#approve) - approveRiskPolicyBypassRequest risk
- [create](#create) - createRiskPolicyBypassRequest risk
- [deny](#deny) - denyRiskPolicyBypassRequest risk
- [list](#list) - listRiskPolicyBypassRequests risk
- [revoke](#revoke) - revokeRiskPolicyBypassRequest risk

## approve

Approve a risk policy bypass request for the requested policy target.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="approveRiskPolicyBypassRequest" method="post" path="/rpc/risk.approvePolicyBypassRequest" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.policyBypassRequests.approve({
    riskPolicyBypassApprovalRequestBody: {
      id: "bc30a025-2871-4321-bd95-3e1f47fd5a8f",
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
import { riskPolicyBypassRequestsApprove } from "@gram/client/funcs/riskPolicyBypassRequestsApprove.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskPolicyBypassRequestsApprove(gram, {
    riskPolicyBypassApprovalRequestBody: {
      id: "bc30a025-2871-4321-bd95-3e1f47fd5a8f",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskPolicyBypassRequestsApprove failed:", res.error);
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
  useRiskApprovePolicyBypassRequestMutation,
} from "@gram/client/react-query/riskPolicyBypassRequestsApprove.js";
```

### Parameters

| Parameter              | Type                                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ApproveRiskPolicyBypassRequestRequest](../../models/operations/approveriskpolicybypassrequestrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ApproveRiskPolicyBypassRequestSecurity](../../models/operations/approveriskpolicybypassrequestsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskPolicyBypassRequest](../../models/components/riskpolicybypassrequest.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## create

Create or refresh a risk policy bypass request from a signed request URL token.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createRiskPolicyBypassRequest" method="post" path="/rpc/risk.createPolicyBypassRequest" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.policyBypassRequests.create({
    createShadowMCPApprovalRequestForm: {
      requestToken: "<value>",
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
import { riskPolicyBypassRequestsCreate } from "@gram/client/funcs/riskPolicyBypassRequestsCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskPolicyBypassRequestsCreate(gram, {
    createShadowMCPApprovalRequestForm: {
      requestToken: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskPolicyBypassRequestsCreate failed:", res.error);
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
  useRiskCreatePolicyBypassRequestMutation,
} from "@gram/client/react-query/riskPolicyBypassRequestsCreate.js";
```

### Parameters

| Parameter              | Type                                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateRiskPolicyBypassRequestRequest](../../models/operations/createriskpolicybypassrequestrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateRiskPolicyBypassRequestSecurity](../../models/operations/createriskpolicybypassrequestsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskPolicyBypassRequest](../../models/components/riskpolicybypassrequest.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deny

Deny a risk policy bypass request, updating workflow state.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="denyRiskPolicyBypassRequest" method="post" path="/rpc/risk.denyPolicyBypassRequest" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.policyBypassRequests.deny({
    riskIDRequestBody: {
      id: "1c7dc61e-2925-4783-a38b-ccfd210b4189",
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
import { riskPolicyBypassRequestsDeny } from "@gram/client/funcs/riskPolicyBypassRequestsDeny.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskPolicyBypassRequestsDeny(gram, {
    riskIDRequestBody: {
      id: "1c7dc61e-2925-4783-a38b-ccfd210b4189",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskPolicyBypassRequestsDeny failed:", res.error);
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
  useRiskDenyPolicyBypassRequestMutation,
} from "@gram/client/react-query/riskPolicyBypassRequestsDeny.js";
```

### Parameters

| Parameter              | Type                                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DenyRiskPolicyBypassRequestRequest](../../models/operations/denyriskpolicybypassrequestrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DenyRiskPolicyBypassRequestSecurity](../../models/operations/denyriskpolicybypassrequestsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskPolicyBypassRequest](../../models/components/riskpolicybypassrequest.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List current risk policy bypass request workflow records.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRiskPolicyBypassRequests" method="get" path="/rpc/risk.listPolicyBypassRequests" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.policyBypassRequests.list();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskPolicyBypassRequestsList } from "@gram/client/funcs/riskPolicyBypassRequestsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskPolicyBypassRequestsList(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskPolicyBypassRequestsList failed:", res.error);
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
  useRiskListPolicyBypassRequests,
  useRiskListPolicyBypassRequestsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskListPolicyBypassRequests,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskListPolicyBypassRequests,
  invalidateAllRiskListPolicyBypassRequests,
} from "@gram/client/react-query/riskPolicyBypassRequestsList.js";
```

### Parameters

| Parameter              | Type                                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListRiskPolicyBypassRequestsRequest](../../models/operations/listriskpolicybypassrequestsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListRiskPolicyBypassRequestsSecurity](../../models/operations/listriskpolicybypassrequestssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListRiskPolicyBypassRequestsResult](../../models/components/listriskpolicybypassrequestsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## revoke

Revoke a previously approved risk policy bypass request.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="revokeRiskPolicyBypassRequest" method="post" path="/rpc/risk.revokePolicyBypassRequest" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.policyBypassRequests.revoke({
    riskIDRequestBody: {
      id: "b52109d2-9378-4d8d-a182-48d0fa3f612e",
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
import { riskPolicyBypassRequestsRevoke } from "@gram/client/funcs/riskPolicyBypassRequestsRevoke.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskPolicyBypassRequestsRevoke(gram, {
    riskIDRequestBody: {
      id: "b52109d2-9378-4d8d-a182-48d0fa3f612e",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskPolicyBypassRequestsRevoke failed:", res.error);
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
  useRiskRevokePolicyBypassRequestMutation,
} from "@gram/client/react-query/riskPolicyBypassRequestsRevoke.js";
```

### Parameters

| Parameter              | Type                                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RevokeRiskPolicyBypassRequestRequest](../../models/operations/revokeriskpolicybypassrequestrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RevokeRiskPolicyBypassRequestSecurity](../../models/operations/revokeriskpolicybypassrequestsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskPolicyBypassRequest](../../models/components/riskpolicybypassrequest.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
