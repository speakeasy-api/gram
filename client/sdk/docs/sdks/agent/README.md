# Agent

## Overview

Endpoints consumed by the Speakeasy device agent running on developer machines. Authenticates via an org-scoped API key carrying the 'agent' scope.

### Available Operations

* [getPlugins](#getplugins) - getPlugins agent

## getPlugins

Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getAgentPlugins" method="get" path="/rpc/agent.getPlugins" -->
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.agent.getPlugins({
    email: "dev@acme.corp",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { agentGetPlugins } from "@gram/client/funcs/agentGetPlugins.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await agentGetPlugins(gram, {
    email: "dev@acme.corp",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("agentGetPlugins failed:", res.error);
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
  useAgentPlugins,
  useAgentPluginsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchAgentPlugins,
  
  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateAgentPlugins,
  invalidateAllAgentPlugins,
} from "@gram/client/react-query/agentGetPlugins.js";
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetAgentPluginsRequest](../../models/operations/getagentpluginsrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `security`                                                                                                                                                                     | [operations.GetAgentPluginsSecurity](../../models/operations/getagentpluginssecurity.md)                                                                                       | :heavy_check_mark:                                                                                                                                                             | The security requirements to use for the request.                                                                                                                              |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.GetPluginsResult](../../models/components/getpluginsresult.md)\>**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ServiceError               | 400, 401, 403, 404, 409, 415, 422 | application/json                  |
| errors.ServiceError               | 500, 502                          | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |