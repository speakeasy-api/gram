# TunneledMcp

## Overview

Managing customer-hosted tunneled MCP servers.

### Available Operations

- [createServer](#createserver) - createServer tunneledMcp
- [deleteServer](#deleteserver) - deleteServer tunneledMcp
- [getServer](#getserver) - getServer tunneledMcp
- [listServerConnections](#listserverconnections) - listServerConnections tunneledMcp
- [listServers](#listservers) - listServers tunneledMcp
- [rotateServerKey](#rotateserverkey) - rotateServerKey tunneledMcp
- [updateServer](#updateserver) - updateServer tunneledMcp

## createServer

Create a new tunneled MCP server source. Returns the tunnel key once.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createTunneledMcpServer" method="post" path="/rpc/tunneledMcp.createServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.createServer({
    createTunneledMcpServerForm: {
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
import { tunneledMcpCreateServer } from "@gram/client/funcs/tunneledMcpCreateServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpCreateServer(gram, {
    createTunneledMcpServerForm: {
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpCreateServer failed:", res.error);
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
  useCreateTunneledMcpServerMutation,
} from "@gram/client/react-query/tunneledMcpCreateServer.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateTunneledMcpServerRequest](../../models/operations/createtunneledmcpserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateTunneledMcpServerSecurity](../../models/operations/createtunneledmcpserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.CreateTunneledMcpServerResult](../../models/components/createtunneledmcpserverresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deleteServer

Delete a tunneled MCP server source

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteTunneledMcpServer" method="delete" path="/rpc/tunneledMcp.deleteServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.tunneledMcp.deleteServer({
    id: "528a5b3d-47d3-4c2f-b008-6a83973013db",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { tunneledMcpDeleteServer } from "@gram/client/funcs/tunneledMcpDeleteServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpDeleteServer(gram, {
    id: "528a5b3d-47d3-4c2f-b008-6a83973013db",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("tunneledMcpDeleteServer failed:", res.error);
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
  useDeleteTunneledMcpServerMutation,
} from "@gram/client/react-query/tunneledMcpDeleteServer.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteTunneledMcpServerRequest](../../models/operations/deletetunneledmcpserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteTunneledMcpServerSecurity](../../models/operations/deletetunneledmcpserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getServer

Get a tunneled MCP server by ID

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getTunneledMcpServer" method="get" path="/rpc/tunneledMcp.getServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.getServer({
    id: "2da0a4eb-6dad-49df-8e77-e695c1ced630",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { tunneledMcpGetServer } from "@gram/client/funcs/tunneledMcpGetServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpGetServer(gram, {
    id: "2da0a4eb-6dad-49df-8e77-e695c1ced630",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpGetServer failed:", res.error);
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
  useGetTunneledMcpServer,
  useGetTunneledMcpServerSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchGetTunneledMcpServer,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateGetTunneledMcpServer,
  invalidateAllGetTunneledMcpServer,
} from "@gram/client/react-query/tunneledMcpGetServer.js";
```

### Parameters

| Parameter              | Type                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetTunneledMcpServerRequest](../../models/operations/gettunneledmcpserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetTunneledMcpServerSecurity](../../models/operations/gettunneledmcpserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TunneledMcpServer](../../models/components/tunneledmcpserver.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listServerConnections

List live tunnel connections for a tunneled MCP server

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listTunneledMcpServerConnections" method="get" path="/rpc/tunneledMcp.listServerConnections" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.listServerConnections({
    id: "c7f0d705-b3f2-47f4-84b2-6eee82b69165",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { tunneledMcpListServerConnections } from "@gram/client/funcs/tunneledMcpListServerConnections.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpListServerConnections(gram, {
    id: "c7f0d705-b3f2-47f4-84b2-6eee82b69165",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpListServerConnections failed:", res.error);
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
  useListTunneledMcpServerConnections,
  useListTunneledMcpServerConnectionsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchListTunneledMcpServerConnections,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateListTunneledMcpServerConnections,
  invalidateAllListTunneledMcpServerConnections,
} from "@gram/client/react-query/tunneledMcpListServerConnections.js";
```

### Parameters

| Parameter              | Type                                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListTunneledMcpServerConnectionsRequest](../../models/operations/listtunneledmcpserverconnectionsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListTunneledMcpServerConnectionsSecurity](../../models/operations/listtunneledmcpserverconnectionssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TunneledMcpServerConnections](../../models/components/tunneledmcpserverconnections.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listServers

List all tunneled MCP server sources for a project

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listTunneledMcpServers" method="get" path="/rpc/tunneledMcp.listServers" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.listServers();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { tunneledMcpListServers } from "@gram/client/funcs/tunneledMcpListServers.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpListServers(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpListServers failed:", res.error);
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
  useTunneledMcpServers,
  useTunneledMcpServersSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchTunneledMcpServers,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateTunneledMcpServers,
  invalidateAllTunneledMcpServers,
} from "@gram/client/react-query/tunneledMcpListServers.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListTunneledMcpServersRequest](../../models/operations/listtunneledmcpserversrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListTunneledMcpServersSecurity](../../models/operations/listtunneledmcpserverssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListTunneledMcpServersResult](../../models/components/listtunneledmcpserversresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## rotateServerKey

Rotate a tunneled MCP server source key. Returns the new tunnel key once.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="rotateTunneledMcpServerKey" method="post" path="/rpc/tunneledMcp.rotateServerKey" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.rotateServerKey({
    rotateTunneledMcpServerKeyForm: {
      id: "f96d4fee-57e8-4a2b-bebc-49247a9a44e1",
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
import { tunneledMcpRotateServerKey } from "@gram/client/funcs/tunneledMcpRotateServerKey.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpRotateServerKey(gram, {
    rotateTunneledMcpServerKeyForm: {
      id: "f96d4fee-57e8-4a2b-bebc-49247a9a44e1",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpRotateServerKey failed:", res.error);
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
  useRotateTunneledMcpServerKeyMutation,
} from "@gram/client/react-query/tunneledMcpRotateServerKey.js";
```

### Parameters

| Parameter              | Type                                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RotateTunneledMcpServerKeyRequest](../../models/operations/rotatetunneledmcpserverkeyrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RotateTunneledMcpServerKeySecurity](../../models/operations/rotatetunneledmcpserverkeysecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RotateTunneledMcpServerKeyResult](../../models/components/rotatetunneledmcpserverkeyresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateServer

Update a tunneled MCP server source

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateTunneledMcpServer" method="post" path="/rpc/tunneledMcp.updateServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.tunneledMcp.updateServer({
    updateTunneledMcpServerForm: {
      id: "e1429b32-4d75-43a3-aa46-02f1af57cf85",
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
import { tunneledMcpUpdateServer } from "@gram/client/funcs/tunneledMcpUpdateServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await tunneledMcpUpdateServer(gram, {
    updateTunneledMcpServerForm: {
      id: "e1429b32-4d75-43a3-aa46-02f1af57cf85",
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("tunneledMcpUpdateServer failed:", res.error);
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
  useUpdateTunneledMcpServerMutation,
} from "@gram/client/react-query/tunneledMcpUpdateServer.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateTunneledMcpServerRequest](../../models/operations/updatetunneledmcpserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateTunneledMcpServerSecurity](../../models/operations/updatetunneledmcpserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.TunneledMcpServer](../../models/components/tunneledmcpserver.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
