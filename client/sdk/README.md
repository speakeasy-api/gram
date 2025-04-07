# gram

Developer-friendly & type-safe Typescript SDK specifically catered to leverage *gram* API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=gram&utm_campaign=typescript"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
    <a href="https://opensource.org/licenses/MIT">
        <img src="https://img.shields.io/badge/License-MIT-blue.svg" style="width: 100px; height: 28px;" />
    </a>
</div>


<br /><br />
> [!IMPORTANT]
> This SDK is not yet ready for production use. To complete setup please follow the steps outlined in your [workspace](https://app.speakeasy.com/org/speakeasy-self/speakeasy-self). Delete this section before > publishing to a package manager.

<!-- Start Summary [summary] -->
## Summary

Gram API Description: Gram is the tools platform for AI agents
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [gram](#gram)
  * [SDK Installation](#sdk-installation)
  * [Requirements](#requirements)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Standalone functions](#standalone-functions)
  * [React hooks with TanStack Query](#react-hooks-with-tanstack-query)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)
  * [Debugging](#debugging)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

> [!TIP]
> To finish publishing your SDK to npm and others you must [run your first generation action](https://www.speakeasy.com/docs/github-setup#step-by-step-guide).


The SDK can be installed with either [npm](https://www.npmjs.com/), [pnpm](https://pnpm.io/), [bun](https://bun.sh/) or [yarn](https://classic.yarnpkg.com/en/) package managers.

### NPM

```bash
npm add <UNSET>
# Install optional peer dependencies if you plan to use React hooks
npm add @tanstack/react-query react react-dom
```

### PNPM

```bash
pnpm add <UNSET>
# Install optional peer dependencies if you plan to use React hooks
pnpm add @tanstack/react-query react react-dom
```

### Bun

```bash
bun add <UNSET>
# Install optional peer dependencies if you plan to use React hooks
bun add @tanstack/react-query react react-dom
```

### Yarn

```bash
yarn add <UNSET> zod
# Install optional peer dependencies if you plan to use React hooks
yarn add @tanstack/react-query react react-dom

# Note that Yarn does not install peer dependencies automatically. You will need
# to install zod as shown above.
```

> [!NOTE]
> This package is published as an ES Module (ESM) only. For applications using
> CommonJS, use `await import()` to import and use this package.

### Model Context Protocol (MCP) Server

This SDK is also an installable MCP server where the various SDK methods are
exposed as tools that can be invoked by AI applications.

> Node.js v20 or greater is required to run the MCP server from npm.

<details>
<summary>Claude installation steps</summary>

Add the following server definition to your `claude_desktop_config.json` file:

```json
{
  "mcpServers": {
    "Gram": {
      "command": "npx",
      "args": [
        "-y", "--package", "@gram/sdk",
        "--",
        "mcp", "start",
        "--project-slug-header-gram-project", "...",
        "--session-header-gram-session", "..."
      ]
    }
  }
}
```

</details>

<details>
<summary>Cursor installation steps</summary>

Create a `.cursor/mcp.json` file in your project root with the following content:

```json
{
  "mcpServers": {
    "Gram": {
      "command": "npx",
      "args": [
        "-y", "--package", "@gram/sdk",
        "--",
        "mcp", "start",
        "--project-slug-header-gram-project", "...",
        "--session-header-gram-session", "..."
      ]
    }
  }
}
```

</details>

You can also run MCP servers as a standalone binary with no additional dependencies. You must pull these binaries from available Github releases:

```bash
curl -L -o mcp-server \
    https://github.com/{org}/{repo}/releases/download/{tag}/mcp-server-bun-darwin-arm64 && \
chmod +x mcp-server
```

If the repo is a private repo you must add your Github PAT to download a release `-H "Authorization: Bearer {GITHUB_PAT}"`.


```json
{
  "mcpServers": {
    "Todos": {
      "command": "./DOWNLOAD/PATH/mcp-server",
      "args": [
        "start"
      ]
    }
  }
}
```

For a full list of server arguments, run:

```sh
npx -y --package @gram/sdk -- mcp start --help
```
<!-- End SDK Installation [installation] -->

<!-- Start Requirements [requirements] -->
## Requirements

For supported JavaScript runtimes, please consult [RUNTIMES.md](RUNTIMES.md).
<!-- End Requirements [requirements] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 924456,
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security schemes globally:

| Name                           | Type   | Scheme  | Environment Variable                    |
| ------------------------------ | ------ | ------- | --------------------------------------- |
| `projectSlugHeaderGramProject` | apiKey | API key | `GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT` |
| `sessionHeaderGramSession`     | apiKey | API key | `GRAM_SESSION_HEADER_GRAM_SESSION`      |

You can set the security parameters through the `security` optional parameter when initializing the SDK client instance. The selected scheme will be used by default to authenticate with the API for all operations that support it. For example:
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 924456,
  });

  // Handle the result
  console.log(result);
}

run();

```

### Per-Operation Security Schemes

Some operations in this SDK require the security scheme to be specified at the request level. For example:
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram();

async function run() {
  const result = await gram.auth.authNumberInfo({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [assets](docs/sdks/assets/README.md)

* [assetsNumberUploadOpenAPIv3](docs/sdks/assets/README.md#assetsnumberuploadopenapiv3) - uploadOpenAPIv3 assets

### [auth](docs/sdks/auth/README.md)

* [authNumberCallback](docs/sdks/auth/README.md#authnumbercallback) - callback auth
* [authNumberInfo](docs/sdks/auth/README.md#authnumberinfo) - info auth
* [authNumberLogout](docs/sdks/auth/README.md#authnumberlogout) - logout auth
* [authNumberSwitchScopes](docs/sdks/auth/README.md#authnumberswitchscopes) - switchScopes auth

### [chat](docs/sdks/chat/README.md)

* [chatNumberCompletion](docs/sdks/chat/README.md#chatnumbercompletion) - completion chat

### [deployments](docs/sdks/deployments/README.md)

* [deploymentsNumberCreateDeployment](docs/sdks/deployments/README.md#deploymentsnumbercreatedeployment) - createDeployment deployments
* [deploymentsNumberGetDeployment](docs/sdks/deployments/README.md#deploymentsnumbergetdeployment) - getDeployment deployments
* [deploymentsNumberListDeployments](docs/sdks/deployments/README.md#deploymentsnumberlistdeployments) - listDeployments deployments

### [environments](docs/sdks/environments/README.md)

* [environmentsNumberCreateEnvironment](docs/sdks/environments/README.md#environmentsnumbercreateenvironment) - createEnvironment environments
* [environmentsNumberDeleteEnvironment](docs/sdks/environments/README.md#environmentsnumberdeleteenvironment) - deleteEnvironment environments
* [environmentsNumberListEnvironments](docs/sdks/environments/README.md#environmentsnumberlistenvironments) - listEnvironments environments
* [environmentsNumberUpdateEnvironment](docs/sdks/environments/README.md#environmentsnumberupdateenvironment) - updateEnvironment environments


### [instances](docs/sdks/instances/README.md)

* [instancesNumberLoadInstance](docs/sdks/instances/README.md#instancesnumberloadinstance) - loadInstance instances

### [keys](docs/sdks/keys/README.md)

* [keysNumberCreateKey](docs/sdks/keys/README.md#keysnumbercreatekey) - createKey keys
* [keysNumberListKeys](docs/sdks/keys/README.md#keysnumberlistkeys) - listKeys keys
* [keysNumberRevokeKey](docs/sdks/keys/README.md#keysnumberrevokekey) - revokeKey keys

### [tools](docs/sdks/tools/README.md)

* [toolsNumberListTools](docs/sdks/tools/README.md#toolsnumberlisttools) - listTools tools

### [toolsets](docs/sdks/toolsets/README.md)

* [toolsetsNumberCreateToolset](docs/sdks/toolsets/README.md#toolsetsnumbercreatetoolset) - createToolset toolsets
* [toolsetsNumberDeleteToolset](docs/sdks/toolsets/README.md#toolsetsnumberdeletetoolset) - deleteToolset toolsets
* [toolsetsNumberGetToolsetDetails](docs/sdks/toolsets/README.md#toolsetsnumbergettoolsetdetails) - getToolsetDetails toolsets
* [toolsetsNumberListToolsets](docs/sdks/toolsets/README.md#toolsetsnumberlisttoolsets) - listToolsets toolsets
* [toolsetsNumberUpdateToolset](docs/sdks/toolsets/README.md#toolsetsnumberupdatetoolset) - updateToolset toolsets

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Standalone functions [standalone-funcs] -->
## Standalone functions

All the methods listed above are available as standalone functions. These
functions are ideal for use in applications running in the browser, serverless
runtimes or other environments where application bundle size is a primary
concern. When using a bundler to build your application, all unused
functionality will be either excluded from the final bundle or tree-shaken away.

To read more about standalone functions, check [FUNCTIONS.md](./FUNCTIONS.md).

<details>

<summary>Available standalone functions</summary>

- [`assetsAssetsNumberUploadOpenAPIv3`](docs/sdks/assets/README.md#assetsnumberuploadopenapiv3) - uploadOpenAPIv3 assets
- [`authAuthNumberCallback`](docs/sdks/auth/README.md#authnumbercallback) - callback auth
- [`authAuthNumberInfo`](docs/sdks/auth/README.md#authnumberinfo) - info auth
- [`authAuthNumberLogout`](docs/sdks/auth/README.md#authnumberlogout) - logout auth
- [`authAuthNumberSwitchScopes`](docs/sdks/auth/README.md#authnumberswitchscopes) - switchScopes auth
- [`chatChatNumberCompletion`](docs/sdks/chat/README.md#chatnumbercompletion) - completion chat
- [`deploymentsDeploymentsNumberCreateDeployment`](docs/sdks/deployments/README.md#deploymentsnumbercreatedeployment) - createDeployment deployments
- [`deploymentsDeploymentsNumberGetDeployment`](docs/sdks/deployments/README.md#deploymentsnumbergetdeployment) - getDeployment deployments
- [`deploymentsDeploymentsNumberListDeployments`](docs/sdks/deployments/README.md#deploymentsnumberlistdeployments) - listDeployments deployments
- [`environmentsEnvironmentsNumberCreateEnvironment`](docs/sdks/environments/README.md#environmentsnumbercreateenvironment) - createEnvironment environments
- [`environmentsEnvironmentsNumberDeleteEnvironment`](docs/sdks/environments/README.md#environmentsnumberdeleteenvironment) - deleteEnvironment environments
- [`environmentsEnvironmentsNumberListEnvironments`](docs/sdks/environments/README.md#environmentsnumberlistenvironments) - listEnvironments environments
- [`environmentsEnvironmentsNumberUpdateEnvironment`](docs/sdks/environments/README.md#environmentsnumberupdateenvironment) - updateEnvironment environments
- [`instancesInstancesNumberLoadInstance`](docs/sdks/instances/README.md#instancesnumberloadinstance) - loadInstance instances
- [`keysKeysNumberCreateKey`](docs/sdks/keys/README.md#keysnumbercreatekey) - createKey keys
- [`keysKeysNumberListKeys`](docs/sdks/keys/README.md#keysnumberlistkeys) - listKeys keys
- [`keysKeysNumberRevokeKey`](docs/sdks/keys/README.md#keysnumberrevokekey) - revokeKey keys
- [`toolsetsToolsetsNumberCreateToolset`](docs/sdks/toolsets/README.md#toolsetsnumbercreatetoolset) - createToolset toolsets
- [`toolsetsToolsetsNumberDeleteToolset`](docs/sdks/toolsets/README.md#toolsetsnumberdeletetoolset) - deleteToolset toolsets
- [`toolsetsToolsetsNumberGetToolsetDetails`](docs/sdks/toolsets/README.md#toolsetsnumbergettoolsetdetails) - getToolsetDetails toolsets
- [`toolsetsToolsetsNumberListToolsets`](docs/sdks/toolsets/README.md#toolsetsnumberlisttoolsets) - listToolsets toolsets
- [`toolsetsToolsetsNumberUpdateToolset`](docs/sdks/toolsets/README.md#toolsetsnumberupdatetoolset) - updateToolset toolsets
- [`toolsToolsNumberListTools`](docs/sdks/tools/README.md#toolsnumberlisttools) - listTools tools

</details>
<!-- End Standalone functions [standalone-funcs] -->

<!-- Start React hooks with TanStack Query [react-query] -->
## React hooks with TanStack Query

React hooks built on [TanStack Query][tanstack-query] are included in this SDK.
These hooks and the utility functions provided alongside them can be used to
build rich applications that pull data from the API using one of the most
popular asynchronous state management library.

[tanstack-query]: https://tanstack.com/query/v5/docs/framework/react/overview

To learn about this feature and how to get started, check
[REACT_QUERY.md](./REACT_QUERY.md).

> [!WARNING]
>
> This feature is currently in **preview** and is subject to breaking changes
> within the current major version of the SDK as we gather user feedback on it.

<details>

<summary>Available React hooks</summary>

- [`useChatCompletionMutation`](docs/sdks/chat/README.md#chatnumbercompletion) - completion chat
- [`useCreateAPIKeyMutation`](docs/sdks/keys/README.md#keysnumbercreatekey) - createKey keys
- [`useCreateDeploymentMutation`](docs/sdks/deployments/README.md#deploymentsnumbercreatedeployment) - createDeployment deployments
- [`useCreateEnvironmentMutation`](docs/sdks/environments/README.md#environmentsnumbercreateenvironment) - createEnvironment environments
- [`useCreateToolsetMutation`](docs/sdks/toolsets/README.md#toolsetsnumbercreatetoolset) - createToolset toolsets
- [`useDeleteEnvironmentMutation`](docs/sdks/environments/README.md#environmentsnumberdeleteenvironment) - deleteEnvironment environments
- [`useDeleteToolsetMutation`](docs/sdks/toolsets/README.md#toolsetsnumberdeletetoolset) - deleteToolset toolsets
- [`useDeployment`](docs/sdks/deployments/README.md#deploymentsnumbergetdeployment) - getDeployment deployments
- [`useListAPIKeys`](docs/sdks/keys/README.md#keysnumberlistkeys) - listKeys keys
- [`useListDeployments`](docs/sdks/deployments/README.md#deploymentsnumberlistdeployments) - listDeployments deployments
- [`useListEnvironments`](docs/sdks/environments/README.md#environmentsnumberlistenvironments) - listEnvironments environments
- [`useListTools`](docs/sdks/tools/README.md#toolsnumberlisttools) - listTools tools
- [`useListToolsets`](docs/sdks/toolsets/README.md#toolsetsnumberlisttoolsets) - listToolsets toolsets
- [`useLoadInstance`](docs/sdks/instances/README.md#instancesnumberloadinstance) - loadInstance instances
- [`useLogout`](docs/sdks/auth/README.md#authnumberlogout) - logout auth
- [`useRevokeAPIKeyMutation`](docs/sdks/keys/README.md#keysnumberrevokekey) - revokeKey keys
- [`useSessionInfo`](docs/sdks/auth/README.md#authnumberinfo) - info auth
- [`useSwitchScopesMutation`](docs/sdks/auth/README.md#authnumberswitchscopes) - switchScopes auth
- [`useToolset`](docs/sdks/toolsets/README.md#toolsetsnumbergettoolsetdetails) - getToolsetDetails toolsets
- [`useUpdateEnvironmentMutation`](docs/sdks/environments/README.md#environmentsnumberupdateenvironment) - updateEnvironment environments
- [`useUpdateToolsetMutation`](docs/sdks/toolsets/README.md#toolsetsnumberupdatetoolset) - updateToolset toolsets
- [`useUploadOpenAPIv3Mutation`](docs/sdks/assets/README.md#assetsnumberuploadopenapiv3) - uploadOpenAPIv3 assets

</details>
<!-- End React hooks with TanStack Query [react-query] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries.  If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API.  However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a retryConfig object to the call:
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 924456,
  }, {
    retries: {
      strategy: "backoff",
      backoff: {
        initialInterval: 1,
        maxInterval: 50,
        exponent: 1.1,
        maxElapsedTime: 100,
      },
      retryConnectionErrors: false,
    },
  });

  // Handle the result
  console.log(result);
}

run();

```

If you'd like to override the default retry strategy for all operations that support retries, you can provide a retryConfig at SDK initialization:
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  retryConfig: {
    strategy: "backoff",
    backoff: {
      initialInterval: 1,
      maxInterval: 50,
      exponent: 1.1,
      maxElapsedTime: 100,
    },
    retryConnectionErrors: false,
  },
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 924456,
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

If the request fails due to, for example 4XX or 5XX status codes, it will throw a `APIError`.

| Error Type      | Status Code | Content Type |
| --------------- | ----------- | ------------ |
| errors.APIError | 4XX, 5XX    | \*/\*        |

```typescript
import { Gram } from "@gram/sdk";
import { SDKValidationError } from "@gram/sdk/models/errors";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  let result;
  try {
    result = await gram.assets.assetsNumberUploadOpenAPIv3({
      contentLength: 924456,
    });

    // Handle the result
    console.log(result);
  } catch (err) {
    switch (true) {
      // The server response does not match the expected SDK schema
      case (err instanceof SDKValidationError):
        {
          // Pretty-print will provide a human-readable multi-line error message
          console.error(err.pretty());
          // Raw value may also be inspected
          console.error(err.rawValue);
          return;
        }
        apierror.js;
      // Server returned an error status code or an unknown content type
      case (err instanceof APIError): {
        console.error(err.statusCode);
        console.error(err.rawResponse.body);
        return;
      }
      default: {
        // Other errors such as network errors, see HTTPClientErrors for more details
        throw err;
      }
    }
  }
}

run();

```

Validation errors can also occur when either method arguments or data returned from the server do not match the expected format. The `SDKValidationError` that is thrown as a result will capture the raw value that failed validation in an attribute called `rawValue`. Additionally, a `pretty()` method is available on this error that can be used to log a nicely formatted multi-line string since validation errors can list many issues and the plain error string may be difficult read when debugging.

In some rare cases, the SDK can fail to get a response from the server or even make the request due to unexpected circumstances such as network conditions. These types of errors are captured in the `models/errors/httpclienterrors.ts` module:

| HTTP Client Error                                    | Description                                          |
| ---------------------------------------------------- | ---------------------------------------------------- |
| RequestAbortedError                                  | HTTP request was aborted by the client               |
| RequestTimeoutError                                  | HTTP request timed out due to an AbortSignal signal  |
| ConnectionError                                      | HTTP client was unable to make a request to a server |
| InvalidRequestError                                  | Any input used to create a request is invalid        |
| UnexpectedClientError                                | Unrecognised or unexpected error                     |
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally by passing a URL to the `serverURL: string` optional parameter when initializing the SDK client instance. For example:
```typescript
import { Gram } from "@gram/sdk";

const gram = new Gram({
  serverURL: "http://localhost:80",
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.assetsNumberUploadOpenAPIv3({
    contentLength: 924456,
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The TypeScript SDK makes API calls using an `HTTPClient` that wraps the native
[Fetch API](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API). This
client is a thin wrapper around `fetch` and provides the ability to attach hooks
around the request lifecycle that can be used to modify the request or handle
errors and response.

The `HTTPClient` constructor takes an optional `fetcher` argument that can be
used to integrate a third-party HTTP client or when writing tests to mock out
the HTTP client and feed in fixtures.

The following example shows how to use the `"beforeRequest"` hook to to add a
custom header and a timeout to requests and how to use the `"requestError"` hook
to log errors:

```typescript
import { Gram } from "@gram/sdk";
import { HTTPClient } from "@gram/sdk/lib/http";

const httpClient = new HTTPClient({
  // fetcher takes a function that has the same signature as native `fetch`.
  fetcher: (request) => {
    return fetch(request);
  }
});

httpClient.addHook("beforeRequest", (request) => {
  const nextRequest = new Request(request, {
    signal: request.signal || AbortSignal.timeout(5000)
  });

  nextRequest.headers.set("x-custom-header", "custom value");

  return nextRequest;
});

httpClient.addHook("requestError", (error, request) => {
  console.group("Request Error");
  console.log("Reason:", `${error}`);
  console.log("Endpoint:", `${request.method} ${request.url}`);
  console.groupEnd();
});

const sdk = new Gram({ httpClient });
```
<!-- End Custom HTTP Client [http-client] -->

<!-- Start Debugging [debug] -->
## Debugging

You can setup your SDK to emit debug logs for SDK requests and responses.

You can pass a logger that matches `console`'s interface as an SDK option.

> [!WARNING]
> Beware that debug logging will reveal secrets, like API tokens in headers, in log messages printed to a console or files. It's recommended to use this feature only during local development and not in production.

```typescript
import { Gram } from "@gram/sdk";

const sdk = new Gram({ debugLogger: console });
```

You can also enable a default debug logger by setting an environment variable `GRAM_DEBUG` to true.
<!-- End Debugging [debug] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation. 
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=gram&utm_campaign=typescript)
