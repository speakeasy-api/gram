# RemoteSessionClients

## Overview

Manage remote_session_client records — credentials Gram uses when acting as an OAuth client of a remote_session_issuer. client_secret_encrypted is never returned.

### Available Operations

- [attachUserSessionIssuer](#attachusersessionissuer) - attachUserSessionIssuer remoteSessionClients
- [cloneClientFromOAuthProxyProvider](#cloneclientfromoauthproxyprovider) - cloneClientFromOAuthProxyProvider remoteSessionClients
- [create](#create) - createRemoteSessionClient remoteSessionClients
- [createCimd](#createcimd) - createCimd remoteSessionClients
- [delete](#delete) - deleteRemoteSessionClient remoteSessionClients
- [detachUserSessionIssuer](#detachusersessionissuer) - detachUserSessionIssuer remoteSessionClients
- [get](#get) - getRemoteSessionClient remoteSessionClients
- [list](#list) - listRemoteSessionClients remoteSessionClients
- [update](#update) - updateRemoteSessionClient remoteSessionClients

## attachUserSessionIssuer

Attach a user_session_issuer to a remote_session_client by recording the binding in the join table. Rejected when another client is already bound to the same user_session_issuer for this client's remote_session_issuer.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="attachUserSessionIssuer" method="post" path="/rpc/remoteSessionClients.attachUserSessionIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.attachUserSessionIssuer({
    attachUserSessionIssuerForm: {
      id: "6c1c491e-d9e7-4da6-81a3-c6c0c8c9ba6b",
      userSessionIssuerId: "7655e5b7-d0fa-4ab1-b5a9-df3ca601f0fe",
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
import { remoteSessionClientsAttachUserSessionIssuer } from "@gram/client/funcs/remoteSessionClientsAttachUserSessionIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsAttachUserSessionIssuer(gram, {
    attachUserSessionIssuerForm: {
      id: "6c1c491e-d9e7-4da6-81a3-c6c0c8c9ba6b",
      userSessionIssuerId: "7655e5b7-d0fa-4ab1-b5a9-df3ca601f0fe",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log(
      "remoteSessionClientsAttachUserSessionIssuer failed:",
      res.error,
    );
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
  useAttachUserSessionIssuerMutation,
} from "@gram/client/react-query/remoteSessionClientsAttachUserSessionIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.AttachUserSessionIssuerRequest](../../models/operations/attachusersessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.AttachUserSessionIssuerSecurity](../../models/operations/attachusersessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## cloneClientFromOAuthProxyProvider

Platform-admin-only. Clone the client_id / client_secret from an existing oauth_proxy_provider into a new remote_session_client paired with the supplied issuers. The upstream secret stays server-side: it is read from the proxy provider's stored secrets, re-encrypted, and persisted on the remote_session_client row without ever crossing the wire.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="cloneClientFromOAuthProxyProvider" method="post" path="/rpc/remoteSessionClients.cloneClientFromOAuthProxyProvider" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result =
    await gram.remoteSessionClients.cloneClientFromOAuthProxyProvider({
      cloneClientFromOAuthProxyProviderForm: {
        oauthProxyProviderId: "4a856623-2d21-4b55-b070-a5607b54b87a",
        remoteSessionIssuerId: "3895728d-43a4-4b97-8f00-c03a994bce37",
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
import { remoteSessionClientsCloneClientFromOAuthProxyProvider } from "@gram/client/funcs/remoteSessionClientsCloneClientFromOAuthProxyProvider.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsCloneClientFromOAuthProxyProvider(
    gram,
    {
      cloneClientFromOAuthProxyProviderForm: {
        oauthProxyProviderId: "4a856623-2d21-4b55-b070-a5607b54b87a",
        remoteSessionIssuerId: "3895728d-43a4-4b97-8f00-c03a994bce37",
      },
    },
  );
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log(
      "remoteSessionClientsCloneClientFromOAuthProxyProvider failed:",
      res.error,
    );
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
  useCloneClientFromOAuthProxyProviderMutation,
} from "@gram/client/react-query/remoteSessionClientsCloneClientFromOAuthProxyProvider.js";
```

### Parameters

| Parameter              | Type                                                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CloneClientFromOAuthProxyProviderRequest](../../models/operations/cloneclientfromoauthproxyproviderrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CloneClientFromOAuthProxyProviderSecurity](../../models/operations/cloneclientfromoauthproxyprovidersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## create

Register a remote_session_client by supplying a client_id and optional client_secret obtained out-of-band from the upstream issuer.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createRemoteSessionClient" method="post" path="/rpc/remoteSessionClients.create" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.create({
    createRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "9c427fdc-c54f-44c3-be91-8986011459d6",
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
import { remoteSessionClientsCreate } from "@gram/client/funcs/remoteSessionClientsCreate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsCreate(gram, {
    createRemoteSessionClientForm: {
      clientId: "<id>",
      remoteSessionIssuerId: "9c427fdc-c54f-44c3-be91-8986011459d6",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("remoteSessionClientsCreate failed:", res.error);
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
  useCreateRemoteSessionClientMutation,
} from "@gram/client/react-query/remoteSessionClientsCreate.js";
```

### Parameters

| Parameter              | Type                                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateRemoteSessionClientRequest](../../models/operations/createremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateRemoteSessionClientSecurity](../../models/operations/createremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## createCimd

Register a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the client carries no secret and authenticates with token_endpoint_auth_method=none. The owning issuer must advertise client_id_metadata_document_supported.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createCimdRemoteSessionClient" method="post" path="/rpc/remoteSessionClients.createCimd" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.createCimd({
    createCimdForm: {
      remoteSessionIssuerId: "69a60658-eb7f-4d4c-b255-ddae9b5f845c",
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
import { remoteSessionClientsCreateCimd } from "@gram/client/funcs/remoteSessionClientsCreateCimd.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsCreateCimd(gram, {
    createCimdForm: {
      remoteSessionIssuerId: "69a60658-eb7f-4d4c-b255-ddae9b5f845c",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("remoteSessionClientsCreateCimd failed:", res.error);
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
  useCreateCimdRemoteSessionClientMutation,
} from "@gram/client/react-query/remoteSessionClientsCreateCimd.js";
```

### Parameters

| Parameter              | Type                                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreateCimdRemoteSessionClientRequest](../../models/operations/createcimdremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreateCimdRemoteSessionClientSecurity](../../models/operations/createcimdremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## delete

Soft-delete a remote_session_client. Cascades to remote_sessions rows pointing at this client; affected principals are forced to re-authenticate.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deleteRemoteSessionClient" method="delete" path="/rpc/remoteSessionClients.delete" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.remoteSessionClients.delete({
    id: "15036b77-1ce1-411c-a3c0-993f40fb5318",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { remoteSessionClientsDelete } from "@gram/client/funcs/remoteSessionClientsDelete.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsDelete(gram, {
    id: "15036b77-1ce1-411c-a3c0-993f40fb5318",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("remoteSessionClientsDelete failed:", res.error);
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
  useDeleteRemoteSessionClientMutation,
} from "@gram/client/react-query/remoteSessionClientsDelete.js";
```

### Parameters

| Parameter              | Type                                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeleteRemoteSessionClientRequest](../../models/operations/deleteremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeleteRemoteSessionClientSecurity](../../models/operations/deleteremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## detachUserSessionIssuer

Detach a user_session_issuer from a remote_session_client by removing the binding from the join table. A no-op when the binding does not exist.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="detachUserSessionIssuer" method="post" path="/rpc/remoteSessionClients.detachUserSessionIssuer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.detachUserSessionIssuer({
    attachUserSessionIssuerForm: {
      id: "1abe0b39-ede5-4534-9ce7-e05cf60087a7",
      userSessionIssuerId: "5c737e42-7d33-4672-90bb-2b871b96b9a6",
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
import { remoteSessionClientsDetachUserSessionIssuer } from "@gram/client/funcs/remoteSessionClientsDetachUserSessionIssuer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsDetachUserSessionIssuer(gram, {
    attachUserSessionIssuerForm: {
      id: "1abe0b39-ede5-4534-9ce7-e05cf60087a7",
      userSessionIssuerId: "5c737e42-7d33-4672-90bb-2b871b96b9a6",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log(
      "remoteSessionClientsDetachUserSessionIssuer failed:",
      res.error,
    );
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
  useDetachUserSessionIssuerMutation,
} from "@gram/client/react-query/remoteSessionClientsDetachUserSessionIssuer.js";
```

### Parameters

| Parameter              | Type                                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DetachUserSessionIssuerRequest](../../models/operations/detachusersessionissuerrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DetachUserSessionIssuerSecurity](../../models/operations/detachusersessionissuersecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## get

Get a remote_session_client by id.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getRemoteSessionClient" method="get" path="/rpc/remoteSessionClients.get" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.get({
    id: "0e9f80fa-56ce-4f95-84f4-54a1493b564e",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { remoteSessionClientsGet } from "@gram/client/funcs/remoteSessionClientsGet.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsGet(gram, {
    id: "0e9f80fa-56ce-4f95-84f4-54a1493b564e",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("remoteSessionClientsGet failed:", res.error);
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
  useRemoteSessionClient,
  useRemoteSessionClientSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRemoteSessionClient,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRemoteSessionClient,
  invalidateAllRemoteSessionClient,
} from "@gram/client/react-query/remoteSessionClientsGet.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetRemoteSessionClientRequest](../../models/operations/getremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetRemoteSessionClientSecurity](../../models/operations/getremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## list

List remote_session_clients in the caller's project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listRemoteSessionClients" method="get" path="/rpc/remoteSessionClients.list" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.list();

  for await (const page of result) {
    console.log(page);
  }
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { remoteSessionClientsList } from "@gram/client/funcs/remoteSessionClientsList.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsList(gram);
  if (res.ok) {
    const { value: result } = res;
    for await (const page of result) {
      console.log(page);
    }
  } else {
    console.log("remoteSessionClientsList failed:", res.error);
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
  useRemoteSessionClients,
  useRemoteSessionClientsSuspense,
  // Query hooks suitable for building infinite scrolling or "load more" UIs.
  useRemoteSessionClientsInfinite,
  useRemoteSessionClientsInfiniteSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchRemoteSessionClients,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateRemoteSessionClients,
  invalidateAllRemoteSessionClients,
} from "@gram/client/react-query/remoteSessionClientsList.js";
```

### Parameters

| Parameter              | Type                                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListRemoteSessionClientsRequest](../../models/operations/listremotesessionclientsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListRemoteSessionClientsSecurity](../../models/operations/listremotesessionclientssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.ListRemoteSessionClientsResponse](../../models/operations/listremotesessionclientsresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## update

Rotate the client_secret or change the non-issuer settings on an existing remote_session_client. Issuer attachments are managed via attachUserSessionIssuer / detachUserSessionIssuer.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateRemoteSessionClient" method="post" path="/rpc/remoteSessionClients.update" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.remoteSessionClients.update({
    updateRemoteSessionClientForm: {
      id: "eb9b6010-ede7-4af2-bb2c-975aff68be3f",
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
import { remoteSessionClientsUpdate } from "@gram/client/funcs/remoteSessionClientsUpdate.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await remoteSessionClientsUpdate(gram, {
    updateRemoteSessionClientForm: {
      id: "eb9b6010-ede7-4af2-bb2c-975aff68be3f",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("remoteSessionClientsUpdate failed:", res.error);
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
  useUpdateRemoteSessionClientMutation,
} from "@gram/client/react-query/remoteSessionClientsUpdate.js";
```

### Parameters

| Parameter              | Type                                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateRemoteSessionClientRequest](../../models/operations/updateremotesessionclientrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateRemoteSessionClientSecurity](../../models/operations/updateremotesessionclientsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.RemoteSessionClient](../../models/components/remotesessionclient.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
