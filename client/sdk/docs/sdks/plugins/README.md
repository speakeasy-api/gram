# Plugins

## Overview

Manage distributable plugin bundles of MCP servers and hooks.

### Available Operations

- [addPluginServer](#addpluginserver) - addPluginServer plugins
- [createPlugin](#createplugin) - createPlugin plugins
- [deletePlugin](#deleteplugin) - deletePlugin plugins
- [downloadCodexInstallScript](#downloadcodexinstallscript) - downloadCodexInstallScript plugins
- [downloadObservabilityPlugin](#downloadobservabilityplugin) - downloadObservabilityPlugin plugins
- [downloadPluginPackage](#downloadpluginpackage) - downloadPluginPackage plugins
- [getMarketplaceSettings](#getmarketplacesettings) - getMarketplaceSettings plugins
- [getPlugin](#getplugin) - getPlugin plugins
- [getPublishStatus](#getpublishstatus) - getPublishStatus plugins
- [listPlugins](#listplugins) - listPlugins plugins
- [publishPlugins](#publishplugins) - publishPlugins plugins
- [removePluginServer](#removepluginserver) - removePluginServer plugins
- [setPluginAssignments](#setpluginassignments) - setPluginAssignments plugins
- [updateMarketplaceSettings](#updatemarketplacesettings) - updateMarketplaceSettings plugins
- [updatePlugin](#updateplugin) - updatePlugin plugins
- [updatePluginServer](#updatepluginserver) - updatePluginServer plugins

## addPluginServer

Add an MCP server to a plugin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="addPluginServer" method="post" path="/rpc/plugins.addPluginServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.addPluginServer({
    addPluginServerForm: {
      pluginId: "cc0fb1d4-a7a9-44fc-9943-cde0f656b019",
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
import { pluginsAddPluginServer } from "@gram/client/funcs/pluginsAddPluginServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsAddPluginServer(gram, {
    addPluginServerForm: {
      pluginId: "cc0fb1d4-a7a9-44fc-9943-cde0f656b019",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsAddPluginServer failed:", res.error);
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
  useAddPluginServerMutation,
} from "@gram/client/react-query/pluginsAddPluginServer.js";
```

### Parameters

| Parameter              | Type                                                                                     | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.AddPluginServerRequest](../../models/operations/addpluginserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.AddPluginServerSecurity](../../models/operations/addpluginserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                           | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)  | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                            | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PluginServer](../../models/components/pluginserver.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## createPlugin

Create a new plugin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="createPlugin" method="post" path="/rpc/plugins.createPlugin" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.createPlugin({
    createPluginForm: {
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
import { pluginsCreatePlugin } from "@gram/client/funcs/pluginsCreatePlugin.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsCreatePlugin(gram, {
    createPluginForm: {
      name: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsCreatePlugin failed:", res.error);
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
  useCreatePluginMutation,
} from "@gram/client/react-query/pluginsCreatePlugin.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.CreatePluginRequest](../../models/operations/createpluginrequest.md)        | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.CreatePluginSecurity](../../models/operations/createpluginsecurity.md)      | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Plugin](../../models/components/plugin.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## deletePlugin

Delete a plugin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="deletePlugin" method="delete" path="/rpc/plugins.deletePlugin" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.plugins.deletePlugin({
    id: "6142055f-198f-4218-a376-7ef9b3022bcd",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsDeletePlugin } from "@gram/client/funcs/pluginsDeletePlugin.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsDeletePlugin(gram, {
    id: "6142055f-198f-4218-a376-7ef9b3022bcd",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("pluginsDeletePlugin failed:", res.error);
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
  useDeletePluginMutation,
} from "@gram/client/react-query/pluginsDeletePlugin.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DeletePluginRequest](../../models/operations/deletepluginrequest.md)        | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DeletePluginSecurity](../../models/operations/deletepluginsecurity.md)      | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
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

## downloadCodexInstallScript

Download a bash install script that registers the Codex observability marketplace and pre-approves all hook events. Requires a published marketplace.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="downloadCodexInstallScript" method="get" path="/rpc/plugins.downloadCodexInstallScript" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.downloadCodexInstallScript();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsDownloadCodexInstallScript } from "@gram/client/funcs/pluginsDownloadCodexInstallScript.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsDownloadCodexInstallScript(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsDownloadCodexInstallScript failed:", res.error);
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
  usePluginsDownloadCodexInstallScript,
  usePluginsDownloadCodexInstallScriptSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPluginsDownloadCodexInstallScript,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePluginsDownloadCodexInstallScript,
  invalidateAllPluginsDownloadCodexInstallScript,
} from "@gram/client/react-query/pluginsDownloadCodexInstallScript.js";
```

### Parameters

| Parameter              | Type                                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DownloadCodexInstallScriptRequest](../../models/operations/downloadcodexinstallscriptrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DownloadCodexInstallScriptSecurity](../../models/operations/downloadcodexinstallscriptsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.DownloadCodexInstallScriptResponse](../../models/operations/downloadcodexinstallscriptresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## downloadObservabilityPlugin

Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="downloadObservabilityPlugin" method="get" path="/rpc/plugins.downloadObservabilityPlugin" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.downloadObservabilityPlugin({
    platform: "cursor",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsDownloadObservabilityPlugin } from "@gram/client/funcs/pluginsDownloadObservabilityPlugin.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsDownloadObservabilityPlugin(gram, {
    platform: "cursor",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsDownloadObservabilityPlugin failed:", res.error);
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
  usePluginsDownloadObservabilityPlugin,
  usePluginsDownloadObservabilityPluginSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPluginsDownloadObservabilityPlugin,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePluginsDownloadObservabilityPlugin,
  invalidateAllPluginsDownloadObservabilityPlugin,
} from "@gram/client/react-query/pluginsDownloadObservabilityPlugin.js";
```

### Parameters

| Parameter              | Type                                                                                                             | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DownloadObservabilityPluginRequest](../../models/operations/downloadobservabilitypluginrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DownloadObservabilityPluginSecurity](../../models/operations/downloadobservabilitypluginsecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                                   | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                          | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                    | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.DownloadObservabilityPluginResponse](../../models/operations/downloadobservabilitypluginresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## downloadPluginPackage

Download a ZIP of a single plugin package for direct installation.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="downloadPluginPackage" method="get" path="/rpc/plugins.downloadPluginPackage" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.downloadPluginPackage({
    pluginId: "433c53a4-80b7-4721-9271-079285149f8e",
    platform: "claude",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsDownloadPluginPackage } from "@gram/client/funcs/pluginsDownloadPluginPackage.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsDownloadPluginPackage(gram, {
    pluginId: "433c53a4-80b7-4721-9271-079285149f8e",
    platform: "claude",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsDownloadPluginPackage failed:", res.error);
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
  usePluginsDownloadPluginPackage,
  usePluginsDownloadPluginPackageSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPluginsDownloadPluginPackage,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePluginsDownloadPluginPackage,
  invalidateAllPluginsDownloadPluginPackage,
} from "@gram/client/react-query/pluginsDownloadPluginPackage.js";
```

### Parameters

| Parameter              | Type                                                                                                 | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.DownloadPluginPackageRequest](../../models/operations/downloadpluginpackagerequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.DownloadPluginPackageSecurity](../../models/operations/downloadpluginpackagesecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                       | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)              | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                        | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[operations.DownloadPluginPackageResponse](../../models/operations/downloadpluginpackageresponse.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getMarketplaceSettings

Get the marketplace settings for the current project, including the effective marketplace name and the server-side default.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getMarketplaceSettings" method="get" path="/rpc/plugins.getMarketplaceSettings" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.getMarketplaceSettings();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsGetMarketplaceSettings } from "@gram/client/funcs/pluginsGetMarketplaceSettings.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsGetMarketplaceSettings(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsGetMarketplaceSettings failed:", res.error);
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
  useMarketplaceSettings,
  useMarketplaceSettingsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchMarketplaceSettings,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidateMarketplaceSettings,
  invalidateAllMarketplaceSettings,
} from "@gram/client/react-query/pluginsGetMarketplaceSettings.js";
```

### Parameters

| Parameter              | Type                                                                                                   | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetMarketplaceSettingsRequest](../../models/operations/getmarketplacesettingsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetMarketplaceSettingsSecurity](../../models/operations/getmarketplacesettingssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                         | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                          | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.MarketplaceSettingsResult](../../models/components/marketplacesettingsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getPlugin

Get a plugin with its servers and assignments.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getPlugin" method="get" path="/rpc/plugins.getPlugin" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.getPlugin({
    id: "2c3fdec1-236d-49ee-a524-9bb3bf5bbcec",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsGetPlugin } from "@gram/client/funcs/pluginsGetPlugin.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsGetPlugin(gram, {
    id: "2c3fdec1-236d-49ee-a524-9bb3bf5bbcec",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsGetPlugin failed:", res.error);
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
  usePlugin,
  usePluginSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPlugin,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePlugin,
  invalidateAllPlugin,
} from "@gram/client/react-query/pluginsGetPlugin.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetPluginRequest](../../models/operations/getpluginrequest.md)              | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetPluginSecurity](../../models/operations/getpluginsecurity.md)            | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Plugin](../../models/components/plugin.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## getPublishStatus

Check whether GitHub publishing is configured and connected for this project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="getPublishStatus" method="get" path="/rpc/plugins.getPublishStatus" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.getPublishStatus();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsGetPublishStatus } from "@gram/client/funcs/pluginsGetPublishStatus.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsGetPublishStatus(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsGetPublishStatus failed:", res.error);
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
  usePublishStatus,
  usePublishStatusSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPublishStatus,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePublishStatus,
  invalidateAllPublishStatus,
} from "@gram/client/react-query/pluginsGetPublishStatus.js";
```

### Parameters

| Parameter              | Type                                                                                       | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.GetPublishStatusRequest](../../models/operations/getpublishstatusrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.GetPublishStatusSecurity](../../models/operations/getpublishstatussecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                             | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)    | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                              | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PublishStatusResult](../../models/components/publishstatusresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## listPlugins

List all plugins for the current project.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="listPlugins" method="get" path="/rpc/plugins.listPlugins" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.listPlugins();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsListPlugins } from "@gram/client/funcs/pluginsListPlugins.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsListPlugins(gram);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsListPlugins failed:", res.error);
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
  usePlugins,
  usePluginsSuspense,

  // Utility for prefetching data during server-side rendering and in React
  // Server Components that will be immediately available to client components
  // using the hooks.
  prefetchPlugins,

  // Utilities to invalidate the query cache for this query in response to
  // mutations and other user actions.
  invalidatePlugins,
  invalidateAllPlugins,
} from "@gram/client/react-query/pluginsListPlugins.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.ListPluginsRequest](../../models/operations/listpluginsrequest.md)          | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.ListPluginsSecurity](../../models/operations/listpluginssecurity.md)        | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ListPluginsResult](../../models/components/listpluginsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## publishPlugins

Generate and publish all plugin packages to a GitHub repository.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="publishPlugins" method="post" path="/rpc/plugins.publishPlugins" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.publishPlugins({
    publishPluginsRequestBody: {},
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsPublishPlugins } from "@gram/client/funcs/pluginsPublishPlugins.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsPublishPlugins(gram, {
    publishPluginsRequestBody: {},
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsPublishPlugins failed:", res.error);
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
  usePublishPluginsMutation,
} from "@gram/client/react-query/pluginsPublishPlugins.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.PublishPluginsRequest](../../models/operations/publishpluginsrequest.md)    | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.PublishPluginsSecurity](../../models/operations/publishpluginssecurity.md)  | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PublishPluginsResult](../../models/components/publishpluginsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## removePluginServer

Remove a server from a plugin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="removePluginServer" method="delete" path="/rpc/plugins.removePluginServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  await gram.plugins.removePluginServer({
    id: "91c5229b-08a3-4bd8-9242-b4205ff5cb2c",
    pluginId: "6f058190-f0b3-450c-8990-3daeadd09c9b",
  });
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsRemovePluginServer } from "@gram/client/funcs/pluginsRemovePluginServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsRemovePluginServer(gram, {
    id: "91c5229b-08a3-4bd8-9242-b4205ff5cb2c",
    pluginId: "6f058190-f0b3-450c-8990-3daeadd09c9b",
  });
  if (res.ok) {
    const { value: result } = res;
  } else {
    console.log("pluginsRemovePluginServer failed:", res.error);
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
  useRemovePluginServerMutation,
} from "@gram/client/react-query/pluginsRemovePluginServer.js";
```

### Parameters

| Parameter              | Type                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.RemovePluginServerRequest](../../models/operations/removepluginserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.RemovePluginServerSecurity](../../models/operations/removepluginserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<void\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## setPluginAssignments

Replace all assignments for a plugin with the given list of principal URNs.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="setPluginAssignments" method="put" path="/rpc/plugins.setPluginAssignments" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.setPluginAssignments({
    setPluginAssignmentsForm: {
      pluginId: "aebf3b89-325e-4bde-9e57-f05ac58d29d8",
      principalUrns: [],
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
import { pluginsSetPluginAssignments } from "@gram/client/funcs/pluginsSetPluginAssignments.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsSetPluginAssignments(gram, {
    setPluginAssignmentsForm: {
      pluginId: "aebf3b89-325e-4bde-9e57-f05ac58d29d8",
      principalUrns: [],
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsSetPluginAssignments failed:", res.error);
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
  useSetPluginAssignmentsMutation,
} from "@gram/client/react-query/pluginsSetPluginAssignments.js";
```

### Parameters

| Parameter              | Type                                                                                               | Required           | Description                                                                                                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.SetPluginAssignmentsRequest](../../models/operations/setpluginassignmentsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.SetPluginAssignmentsSecurity](../../models/operations/setpluginassignmentssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                     | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)            | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                      | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SetPluginAssignmentsResponseBody](../../models/components/setpluginassignmentsresponsebody.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updateMarketplaceSettings

Update the marketplace settings for the current project. If a marketplace is already published, the updated settings are pushed to GitHub before the call returns.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateMarketplaceSettings" method="post" path="/rpc/plugins.updateMarketplaceSettings" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.updateMarketplaceSettings({
    updateMarketplaceSettingsRequestBody: {},
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { GramCore } from "@gram/client/core.js";
import { pluginsUpdateMarketplaceSettings } from "@gram/client/funcs/pluginsUpdateMarketplaceSettings.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsUpdateMarketplaceSettings(gram, {
    updateMarketplaceSettingsRequestBody: {},
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsUpdateMarketplaceSettings failed:", res.error);
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
  useUpdateMarketplaceSettingsMutation,
} from "@gram/client/react-query/pluginsUpdateMarketplaceSettings.js";
```

### Parameters

| Parameter              | Type                                                                                                         | Required           | Description                                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdateMarketplaceSettingsRequest](../../models/operations/updatemarketplacesettingsrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdateMarketplaceSettingsSecurity](../../models/operations/updatemarketplacesettingssecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                               | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                      | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                                | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.UpdateMarketplaceSettingsResult](../../models/components/updatemarketplacesettingsresult.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updatePlugin

Update plugin metadata.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updatePlugin" method="put" path="/rpc/plugins.updatePlugin" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.updatePlugin({
    updatePluginForm: {
      id: "1c0290eb-9c44-4e10-bcbb-55a9ea8ae57f",
      name: "<value>",
      slug: "<value>",
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
import { pluginsUpdatePlugin } from "@gram/client/funcs/pluginsUpdatePlugin.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsUpdatePlugin(gram, {
    updatePluginForm: {
      id: "1c0290eb-9c44-4e10-bcbb-55a9ea8ae57f",
      name: "<value>",
      slug: "<value>",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsUpdatePlugin failed:", res.error);
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
  useUpdatePluginMutation,
} from "@gram/client/react-query/pluginsUpdatePlugin.js";
```

### Parameters

| Parameter              | Type                                                                                    | Required           | Description                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdatePluginRequest](../../models/operations/updatepluginrequest.md)        | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdatePluginSecurity](../../models/operations/updatepluginsecurity.md)      | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                          | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options) | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                           | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Plugin](../../models/components/plugin.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

## updatePluginServer

Update a server's configuration within a plugin.

### Example Usage

<!-- UsageSnippet language="typescript" operationID="updatePluginServer" method="put" path="/rpc/plugins.updatePluginServer" -->

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.plugins.updatePluginServer({
    updatePluginServerForm: {
      displayName: "Claire85",
      id: "a8ec9533-2162-4aa5-83d0-e6eb96a97c2e",
      pluginId: "522e6141-9ed3-4258-9625-68a5bd1c7ba7",
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
import { pluginsUpdatePluginServer } from "@gram/client/funcs/pluginsUpdatePluginServer.js";

// Use `GramCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const gram = new GramCore();

async function run() {
  const res = await pluginsUpdatePluginServer(gram, {
    updatePluginServerForm: {
      displayName: "Claire85",
      id: "a8ec9533-2162-4aa5-83d0-e6eb96a97c2e",
      pluginId: "522e6141-9ed3-4258-9625-68a5bd1c7ba7",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("pluginsUpdatePluginServer failed:", res.error);
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
  useUpdatePluginServerMutation,
} from "@gram/client/react-query/pluginsUpdatePluginServer.js";
```

### Parameters

| Parameter              | Type                                                                                           | Required           | Description                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`              | [operations.UpdatePluginServerRequest](../../models/operations/updatepluginserverrequest.md)   | :heavy_check_mark: | The request object to use for the request.                                                                                                                                     |
| `security`             | [operations.UpdatePluginServerSecurity](../../models/operations/updatepluginserversecurity.md) | :heavy_check_mark: | The security requirements to use for the request.                                                                                                                              |
| `options`              | RequestOptions                                                                                 | :heavy_minus_sign: | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions` | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)        | :heavy_minus_sign: | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`      | [RetryConfig](../../lib/utils/retryconfig.md)                                                  | :heavy_minus_sign: | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PluginServer](../../models/components/pluginserver.md)\>**

### Errors

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500, 502                          | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |
