# Telemetry

## Overview

Fetch telemetry data for tools in Gram.

### Available Operations

* [captureEvent](#captureevent) - captureEvent telemetry
* [getEmployeeDataFlowGraph](#getemployeedataflowgraph) - getEmployeeDataFlowGraph telemetry
* [getHooksSummary](#gethookssummary) - getHooksSummary telemetry
* [getObservabilityOverview](#getobservabilityoverview) - getObservabilityOverview telemetry
* [getProjectMetricsSummary](#getprojectmetricssummary) - getProjectMetricsSummary telemetry
* [getProjectOverview](#getprojectoverview) - getProjectOverview telemetry
* [getToolUsageFilterOptions](#gettoolusagefilteroptions) - getToolUsageFilterOptions telemetry
* [getToolUsageSummary](#gettoolusagesummary) - getToolUsageSummary telemetry
* [getUserMetricsSummary](#getusermetricssummary) - getUserMetricsSummary telemetry
* [listAttributeKeys](#listattributekeys) - listAttributeKeys telemetry
* [listFilterOptions](#listfilteroptions) - listFilterOptions telemetry
* [listHooksTraces](#listhookstraces) - listHooksTraces telemetry
* [listSessions](#listsessions) - listSessions telemetry
* [listToolUsageTraces](#listtoolusagetraces) - listToolUsageTraces telemetry
* [query](#query) - query telemetry
* [searchChats](#searchchats) - searchChats telemetry
* [searchLogs](#searchlogs) - searchLogs telemetry
* [searchToolCalls](#searchtoolcalls) - searchToolCalls telemetry
* [searchUsers](#searchusers) - searchUsers telemetry

## captureEvent

Capture a telemetry event and forward it to PostHog

### Example Usage

<!-- UsageSnippet language="typescript" operationID="captureEvent" method="post" path="/rpc/telemetry.captureEvent" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.captureEvent({
    captureEventPayload: {
      event: "button_clicked",
      properties: {
        "button_name": "submit",
        "page": "checkout",
        "value": 100,
      },
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
import { telemetryCaptureEvent } from "@gram/client/funcs/telemetryCaptureEvent.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryCaptureEvent(gram, {
    captureEventPayload: {
      event: "button_clicked",
      properties: {
        "button_name": "submit",
        "page": "checkout",
        "value": 100,
      },
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryCaptureEvent failed:", res.error);
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
  useTelemetryCaptureEventMutation
} from "@gram/client/react-query/telemetryCaptureEvent.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.CaptureEventRequest](../../models/operations/captureeventrequest.md)                                                                                               | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.CaptureEventSecurity](../../models/operations/captureeventsecurity.md)                                                                                             | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.CaptureEventResult](../../models/components/captureeventresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getEmployeeDataFlowGraph

Get an employee's MCP data flow graph across origins, clients, servers, and tools

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getEmployeeDataFlowGraph" method="post" path="/rpc/telemetry.getEmployeeDataFlowGraph" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getEmployeeDataFlowGraph({
    getEmployeeDataFlowGraphPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetEmployeeDataFlowGraph } from "@gram/client/funcs/telemetryGetEmployeeDataFlowGraph.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetEmployeeDataFlowGraph(gram, {
    getEmployeeDataFlowGraphPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetEmployeeDataFlowGraph failed:", res.error);
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
  useGetEmployeeDataFlowGraph,
  useGetEmployeeDataFlowGraphSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetEmployeeDataFlowGraph,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetEmployeeDataFlowGraph,
  invalidateAllGetEmployeeDataFlowGraph,
} from "@gram/client/react-query/telemetryGetEmployeeDataFlowGraph.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetEmployeeDataFlowGraphRequest](../../models/operations/getemployeedataflowgraphrequest.md)                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetEmployeeDataFlowGraphSecurity](../../models/operations/getemployeedataflowgraphsecurity.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetEmployeeDataFlowGraphResult](../../models/components/getemployeedataflowgraphresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getHooksSummary

Get aggregated hooks metrics grouped by server

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getHooksSummary" method="post" path="/rpc/telemetry.getHooksSummary" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getHooksSummary({
    getHooksSummaryPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
      typesToInclude: [
        "mcp",
        "skill",
      ],
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
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetHooksSummary(gram, {
    getHooksSummaryPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
      typesToInclude: [
        "mcp",
        "skill",
      ],
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetHooksSummary failed:", res.error);
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
  useGetHooksSummary,
  useGetHooksSummarySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetHooksSummary,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetHooksSummary,
  invalidateAllGetHooksSummary,
} from "@gram/client/react-query/telemetryGetHooksSummary.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetHooksSummaryRequest](../../models/operations/gethookssummaryrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetHooksSummarySecurity](../../models/operations/gethookssummarysecurity.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetHooksSummaryResult](../../models/components/gethookssummaryresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getObservabilityOverview

Get observability overview metrics including time series, tool breakdowns, and summary stats

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getObservabilityOverview" method="post" path="/rpc/telemetry.getObservabilityOverview" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getObservabilityOverview({
    getObservabilityOverviewPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetObservabilityOverview(gram, {
    getObservabilityOverviewPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetObservabilityOverview failed:", res.error);
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
  useGetObservabilityOverview,
  useGetObservabilityOverviewSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetObservabilityOverview,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetObservabilityOverview,
  invalidateAllGetObservabilityOverview,
} from "@gram/client/react-query/telemetryGetObservabilityOverview.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetObservabilityOverviewRequest](../../models/operations/getobservabilityoverviewrequest.md)                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetObservabilityOverviewSecurity](../../models/operations/getobservabilityoverviewsecurity.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetObservabilityOverviewResult](../../models/components/getobservabilityoverviewresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getProjectMetricsSummary

Get aggregated metrics summary for an entire project

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getProjectMetricsSummary" method="post" path="/rpc/telemetry.getProjectMetricsSummary" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getProjectMetricsSummary({
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetProjectMetricsSummary } from "@gram/client/funcs/telemetryGetProjectMetricsSummary.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetProjectMetricsSummary(gram, {
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetProjectMetricsSummary failed:", res.error);
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
  useGetProjectMetricsSummary,
  useGetProjectMetricsSummarySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetProjectMetricsSummary,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetProjectMetricsSummary,
  invalidateAllGetProjectMetricsSummary,
} from "@gram/client/react-query/telemetryGetProjectMetricsSummary.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetProjectMetricsSummaryRequest](../../models/operations/getprojectmetricssummaryrequest.md)                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetProjectMetricsSummarySecurity](../../models/operations/getprojectmetricssummarysecurity.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetMetricsSummaryResult](../../models/components/getmetricssummaryresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getProjectOverview

Get project-level overview including total chats, tool calls, active servers/users, and top lists

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getProjectOverview" method="post" path="/rpc/telemetry.getProjectOverview" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getProjectOverview({
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetProjectOverview } from "@gram/client/funcs/telemetryGetProjectOverview.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetProjectOverview(gram, {
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetProjectOverview failed:", res.error);
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
  useGetProjectOverview,
  useGetProjectOverviewSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetProjectOverview,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetProjectOverview,
  invalidateAllGetProjectOverview,
} from "@gram/client/react-query/telemetryGetProjectOverview.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetProjectOverviewRequest](../../models/operations/getprojectoverviewrequest.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetProjectOverviewSecurity](../../models/operations/getprojectoverviewsecurity.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetProjectOverviewResult](../../models/components/getprojectoverviewresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getToolUsageFilterOptions

Get filter options for target-aware MCP and tool usage metrics

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getToolUsageFilterOptions" method="post" path="/rpc/telemetry.getToolUsageFilterOptions" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getToolUsageFilterOptions({
    getToolUsageFilterOptionsPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetToolUsageFilterOptions } from "@gram/client/funcs/telemetryGetToolUsageFilterOptions.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetToolUsageFilterOptions(gram, {
    getToolUsageFilterOptionsPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetToolUsageFilterOptions failed:", res.error);
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
  useGetToolUsageFilterOptions,
  useGetToolUsageFilterOptionsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetToolUsageFilterOptions,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetToolUsageFilterOptions,
  invalidateAllGetToolUsageFilterOptions,
} from "@gram/client/react-query/telemetryGetToolUsageFilterOptions.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetToolUsageFilterOptionsRequest](../../models/operations/gettoolusagefilteroptionsrequest.md)                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetToolUsageFilterOptionsSecurity](../../models/operations/gettoolusagefilteroptionssecurity.md)                                                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetToolUsageFilterOptionsResult](../../models/components/gettoolusagefilteroptionsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getToolUsageSummary

Get target-aware MCP and tool usage metrics

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getToolUsageSummary" method="post" path="/rpc/telemetry.getToolUsageSummary" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getToolUsageSummary({
    getToolUsageSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetToolUsageSummary } from "@gram/client/funcs/telemetryGetToolUsageSummary.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetToolUsageSummary(gram, {
    getToolUsageSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetToolUsageSummary failed:", res.error);
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
  useGetToolUsageSummary,
  useGetToolUsageSummarySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetToolUsageSummary,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetToolUsageSummary,
  invalidateAllGetToolUsageSummary,
} from "@gram/client/react-query/telemetryGetToolUsageSummary.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetToolUsageSummaryRequest](../../models/operations/gettoolusagesummaryrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetToolUsageSummarySecurity](../../models/operations/gettoolusagesummarysecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetToolUsageSummaryResult](../../models/components/gettoolusagesummaryresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## getUserMetricsSummary

Get aggregated metrics summary grouped by user

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getUserMetricsSummary" method="post" path="/rpc/telemetry.getUserMetricsSummary" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.getUserMetricsSummary({
    getUserMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryGetUserMetricsSummary } from "@gram/client/funcs/telemetryGetUserMetricsSummary.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryGetUserMetricsSummary(gram, {
    getUserMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryGetUserMetricsSummary failed:", res.error);
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
  useGetUserMetricsSummary,
  useGetUserMetricsSummarySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetUserMetricsSummary,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetUserMetricsSummary,
  invalidateAllGetUserMetricsSummary,
} from "@gram/client/react-query/telemetryGetUserMetricsSummary.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetUserMetricsSummaryRequest](../../models/operations/getusermetricssummaryrequest.md)                                                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetUserMetricsSummarySecurity](../../models/operations/getusermetricssummarysecurity.md)                                                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetUserMetricsSummaryResult](../../models/components/getusermetricssummaryresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listAttributeKeys

List distinct attribute keys available for filtering

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listAttributeKeys" method="post" path="/rpc/telemetry.listAttributeKeys" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.listAttributeKeys({
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryListAttributeKeys } from "@gram/client/funcs/telemetryListAttributeKeys.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryListAttributeKeys(gram, {
    getProjectMetricsSummaryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryListAttributeKeys failed:", res.error);
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
  useListAttributeKeys,
  useListAttributeKeysSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListAttributeKeys,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListAttributeKeys,
  invalidateAllListAttributeKeys,
} from "@gram/client/react-query/telemetryListAttributeKeys.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListAttributeKeysRequest](../../models/operations/listattributekeysrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListAttributeKeysSecurity](../../models/operations/listattributekeyssecurity.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListAttributeKeysResult](../../models/components/listattributekeysresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listFilterOptions

List available filter options (API keys or users) for the observability overview

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listFilterOptions" method="post" path="/rpc/telemetry.listFilterOptions" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.listFilterOptions({
    listFilterOptionsPayload: {
      filterType: "user",
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryListFilterOptions } from "@gram/client/funcs/telemetryListFilterOptions.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryListFilterOptions(gram, {
    listFilterOptionsPayload: {
      filterType: "user",
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryListFilterOptions failed:", res.error);
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
  useListFilterOptions,
  useListFilterOptionsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListFilterOptions,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListFilterOptions,
  invalidateAllListFilterOptions,
} from "@gram/client/react-query/telemetryListFilterOptions.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListFilterOptionsRequest](../../models/operations/listfilteroptionsrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListFilterOptionsSecurity](../../models/operations/listfilteroptionssecurity.md)                                                                                   | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListFilterOptionsResult](../../models/components/listfilteroptionsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listHooksTraces

List hook traces aggregated by trace_id with user information

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listHooksTraces" method="post" path="/rpc/telemetry.listHooksTraces" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.listHooksTraces({
    listHooksTracesPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
      typesToInclude: [
        "mcp",
        "skill",
      ],
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
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryListHooksTraces(gram, {
    listHooksTracesPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
      typesToInclude: [
        "mcp",
        "skill",
      ],
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryListHooksTraces failed:", res.error);
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
  useListHooksTraces,
  useListHooksTracesSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListHooksTraces,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListHooksTraces,
  invalidateAllListHooksTraces,
} from "@gram/client/react-query/telemetryListHooksTraces.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListHooksTracesRequest](../../models/operations/listhookstracesrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListHooksTracesSecurity](../../models/operations/listhookstracessecurity.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListHooksTracesResult](../../models/components/listhookstracesresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listSessions

Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listSessions" method="post" path="/rpc/telemetry.listSessions" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.listSessions({
    listSessionsPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-26T10:00:00Z"),
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
import { telemetryListSessions } from "@gram/client/funcs/telemetryListSessions.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryListSessions(gram, {
    listSessionsPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-26T10:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryListSessions failed:", res.error);
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
  useListSessions,
  useListSessionsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListSessions,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListSessions,
  invalidateAllListSessions,
} from "@gram/client/react-query/telemetryListSessions.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListSessionsRequest](../../models/operations/listsessionsrequest.md)                                                                                               | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListSessionsSecurity](../../models/operations/listsessionssecurity.md)                                                                                             | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListSessionsResult](../../models/components/listsessionsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## listToolUsageTraces

List target-aware MCP and tool usage traces

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listToolUsageTraces" method="post" path="/rpc/telemetry.listToolUsageTraces" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.listToolUsageTraces({
    listToolUsageTracesPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetryListToolUsageTraces } from "@gram/client/funcs/telemetryListToolUsageTraces.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryListToolUsageTraces(gram, {
    listToolUsageTracesPayload: {
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryListToolUsageTraces failed:", res.error);
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
  useListToolUsageTraces,
  useListToolUsageTracesSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListToolUsageTraces,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListToolUsageTraces,
  invalidateAllListToolUsageTraces,
} from "@gram/client/react-query/telemetryListToolUsageTraces.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.ListToolUsageTracesRequest](../../models/operations/listtoolusagetracesrequest.md)                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.ListToolUsageTracesSecurity](../../models/operations/listtoolusagetracessecurity.md)                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListToolUsageTracesResult](../../models/components/listtoolusagetracesresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## query

Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).

### Example Usage

<!-- UsageSnippet language="typescript" operationID="query" method="post" path="/rpc/telemetry.query" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.query({
    queryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      groupBy: "department_name",
      to: new Date("2025-12-26T10:00:00Z"),
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
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetryQuery(gram, {
    queryPayload: {
      from: new Date("2025-12-19T10:00:00Z"),
      groupBy: "department_name",
      to: new Date("2025-12-26T10:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetryQuery failed:", res.error);
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
  useTelemetryQuery,
  useTelemetryQuerySuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchTelemetryQuery,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateTelemetryQuery,
  invalidateAllTelemetryQuery,
} from "@gram/client/react-query/telemetryQuery.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.QueryRequest](../../models/operations/queryrequest.md)                                                                                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.QuerySecurity](../../models/operations/querysecurity.md)                                                                                                           | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.QueryResult](../../models/components/queryresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## searchChats

Search and list chat session summaries that match a search filter

### Example Usage

<!-- UsageSnippet language="typescript" operationID="searchChats" method="post" path="/rpc/telemetry.searchChats" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.searchChats({
    searchChatsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
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
import { telemetrySearchChats } from "@gram/client/funcs/telemetrySearchChats.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetrySearchChats(gram, {
    searchChatsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetrySearchChats failed:", res.error);
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
  useSearchChats,
  useSearchChatsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchSearchChats,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateSearchChats,
  invalidateAllSearchChats,
} from "@gram/client/react-query/telemetrySearchChats.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.SearchChatsRequest](../../models/operations/searchchatsrequest.md)                                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.SearchChatsSecurity](../../models/operations/searchchatssecurity.md)                                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SearchChatsResult](../../models/components/searchchatsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## searchLogs

Search and list telemetry logs that match a search filter

### Example Usage

<!-- UsageSnippet language="typescript" operationID="searchLogs" method="post" path="/rpc/telemetry.searchLogs" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.searchLogs({
    searchLogsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
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
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetrySearchLogs(gram, {
    searchLogsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
      filters: [
        {
          path: "@user.region",
        },
      ],
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetrySearchLogs failed:", res.error);
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
  useSearchLogsMutation
} from "@gram/client/react-query/telemetrySearchLogs.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.SearchLogsRequest](../../models/operations/searchlogsrequest.md)                                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.SearchLogsSecurity](../../models/operations/searchlogssecurity.md)                                                                                                 | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SearchLogsResult](../../models/components/searchlogsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## searchToolCalls

Search and list tool calls that match a search filter

### Example Usage

<!-- UsageSnippet language="typescript" operationID="searchToolCalls" method="post" path="/rpc/telemetry.searchToolCalls" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.searchToolCalls({
    searchToolCallsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
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
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetrySearchToolCalls(gram, {
    searchToolCallsPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetrySearchToolCalls failed:", res.error);
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
  useSearchToolCallsMutation
} from "@gram/client/react-query/telemetrySearchToolCalls.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.SearchToolCallsRequest](../../models/operations/searchtoolcallsrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.SearchToolCallsSecurity](../../models/operations/searchtoolcallssecurity.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SearchToolCallsResult](../../models/components/searchtoolcallsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## searchUsers

Search and list user usage summaries grouped by user_id or external_user_id

### Example Usage

<!-- UsageSnippet language="typescript" operationID="searchUsers" method="post" path="/rpc/telemetry.searchUsers" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.telemetry.searchUsers({
    searchUsersPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
      userType: "external",
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
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await telemetrySearchUsers(gram, {
    searchUsersPayload: {
      filter: {
        from: new Date("2025-12-19T10:00:00Z"),
        to: new Date("2025-12-19T11:00:00Z"),
      },
      userType: "external",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("telemetrySearchUsers failed:", res.error);
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
  useSearchUsers,
  useSearchUsersSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchSearchUsers,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateSearchUsers,
  invalidateAllSearchUsers,
} from "@gram/client/react-query/telemetrySearchUsers.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.SearchUsersRequest](../../models/operations/searchusersrequest.md)                                                                                                 | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.SearchUsersSecurity](../../models/operations/searchuserssecurity.md)                                                                                               | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SearchUsersResult](../../models/components/searchusersresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |