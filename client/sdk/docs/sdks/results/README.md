# Risk.Results

## Overview

### Available Operations

- [list](#list) - listRiskResults risk
- [byChat](#bychat) - listRiskResultsByChat risk
- [listForAgent](#listforagent) - listRiskResultsForAgent risk
- [unmask](#unmask) - unmaskRiskResult risk

## list

List risk analysis results for the current project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRiskResults" method="get" path="/rpc/risk.listResults" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.results.list();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskResultsList } from "@gram/client/funcs/riskResultsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskResultsList(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskResultsList failed:", res.error);
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
  useRiskListResults,
  useRiskListResultsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskListResults,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskListResults,
  invalidateAllRiskListResults,
} from "@gram/client/react-query/riskResultsList.js";
```

### Parameters

| Parameter              | Type                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListRiskResultsRequest](../../models/operations/listriskresultsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListRiskResultsSecurity](../../models/operations/listriskresultssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListRiskResultsResult](../../models/components/listriskresultsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## byChat

List risk results grouped by chat session for the current project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRiskResultsByChat" method="get" path="/rpc/risk.listResultsByChat" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.results.byChat();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskResultsByChat } from "@gram/client/funcs/riskResultsByChat.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskResultsByChat(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskResultsByChat failed:", res.error);
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
  useRiskListResultsByChat,
  useRiskListResultsByChatSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskListResultsByChat,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskListResultsByChat,
  invalidateAllRiskListResultsByChat,
} from "@gram/client/react-query/riskResultsByChat.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListRiskResultsByChatRequest](../../models/operations/listriskresultsbychatrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListRiskResultsByChatSecurity](../../models/operations/listriskresultsbychatsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListRiskResultsByChatResult](../../models/components/listriskresultsbychatresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listForAgent

List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRiskResultsForAgent" method="get" path="/rpc/risk.listResultsForAgent" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.results.listForAgent();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { riskResultsListForAgent } from "@gram/client/funcs/riskResultsListForAgent.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskResultsListForAgent(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskResultsListForAgent failed:", res.error);
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
  useRiskListResultsForAgent,
  useRiskListResultsForAgentSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRiskListResultsForAgent,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRiskListResultsForAgent,
  invalidateAllRiskListResultsForAgent,
} from "@gram/client/react-query/riskResultsListForAgent.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListRiskResultsForAgentRequest](../../models/operations/listriskresultsforagentrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListRiskResultsForAgentSecurity](../../models/operations/listriskresultsforagentsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListRiskResultsForAgentResult](../../models/components/listriskresultsforagentresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## unmask

Return the plaintext match for a single risk result, on demand. Gated on the chat:read scope for the result's chat (not org:admin) — reveal is a discrete, audited access event distinct from listing redacted results.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="unmaskRiskResult" method="post" path="/rpc/risk.unmaskResult" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.risk.results.unmask({
    riskIDRequestBody: {
      id: "62812167-ce5a-438e-bcc0-10d7ab10b11f",
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
import { riskResultsUnmask } from "@gram/client/funcs/riskResultsUnmask.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await riskResultsUnmask(gram, {
    riskIDRequestBody: {
      id: "62812167-ce5a-438e-bcc0-10d7ab10b11f",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("riskResultsUnmask failed:", res.error);
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
  useRiskUnmaskResultMutation,
} from "@gram/client/react-query/riskResultsUnmask.js";
```

### Parameters

| Parameter              | Type                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UnmaskRiskResultRequest](../../models/operations/unmaskriskresultrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UnmaskRiskResultSecurity](../../models/operations/unmaskriskresultsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RiskUnmaskResultResult](../../models/components/riskunmaskresultresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
