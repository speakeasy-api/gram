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
<!-- End SDK Installation [installation] -->

<!-- Start Requirements [requirements] -->
## Requirements

For supported JavaScript runtimes, please consult [RUNTIMES.md](RUNTIMES.md).
<!-- End Requirements [requirements] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assets.serveImage({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  }, {
    id: "<id>",
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
import { Gram } from "@gram/client";

const gram = new Gram({
  security: {
    projectSlugHeaderGramProject:
      process.env["GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT"] ?? "",
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  },
});

async function run() {
  const result = await gram.assets.uploadImage({
    contentLength: 461855,
  });

  // Handle the result
  console.log(result);
}

run();

```

### Per-Operation Security Schemes

Some operations in this SDK require the security scheme to be specified at the request level. For example:
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assets.serveImage({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  }, {
    id: "<id>",
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

* [serveImage](docs/sdks/assets/README.md#serveimage) - serveImage assets
* [uploadImage](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
* [uploadOpenAPIv3](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets

### [auth](docs/sdks/auth/README.md)

* [callback](docs/sdks/auth/README.md#callback) - callback auth
* [info](docs/sdks/auth/README.md#info) - info auth
* [login](docs/sdks/auth/README.md#login) - login auth
* [logout](docs/sdks/auth/README.md#logout) - logout auth
* [switchScopes](docs/sdks/auth/README.md#switchscopes) - switchScopes auth

### [chat](docs/sdks/chat/README.md)

* [list](docs/sdks/chat/README.md#list) - listChats chat
* [load](docs/sdks/chat/README.md#load) - loadChat chat

### [deployments](docs/sdks/deployments/README.md)

* [create](docs/sdks/deployments/README.md#create) - createDeployment deployments
* [evolveDeployment](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
* [getById](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
* [latest](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
* [list](docs/sdks/deployments/README.md#list) - listDeployments deployments

### [environments](docs/sdks/environments/README.md)

* [create](docs/sdks/environments/README.md#create) - createEnvironment environments
* [deleteBySlug](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
* [list](docs/sdks/environments/README.md#list) - listEnvironments environments
* [updateBySlug](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments


### [instances](docs/sdks/instances/README.md)

* [getBySlug](docs/sdks/instances/README.md#getbyslug) - getInstance instances

### [integrations](docs/sdks/integrations/README.md)

* [integrationsNumberGet](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
* [list](docs/sdks/integrations/README.md#list) - list integrations

### [keys](docs/sdks/keys/README.md)

* [create](docs/sdks/keys/README.md#create) - createKey keys
* [list](docs/sdks/keys/README.md#list) - listKeys keys
* [revokeById](docs/sdks/keys/README.md#revokebyid) - revokeKey keys

### [packages](docs/sdks/packages/README.md)

* [create](docs/sdks/packages/README.md#create) - createPackage packages
* [listVersions](docs/sdks/packages/README.md#listversions) - listVersions packages
* [publish](docs/sdks/packages/README.md#publish) - publish packages
* [update](docs/sdks/packages/README.md#update) - updatePackage packages

### [projects](docs/sdks/projects/README.md)

* [create](docs/sdks/projects/README.md#create) - createProject projects
* [list](docs/sdks/projects/README.md#list) - listProjects projects

### [tools](docs/sdks/tools/README.md)

* [list](docs/sdks/tools/README.md#list) - listTools tools

### [toolsets](docs/sdks/toolsets/README.md)

* [create](docs/sdks/toolsets/README.md#create) - createToolset toolsets
* [deleteBySlug](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
* [getBySlug](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
* [list](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
* [updateBySlug](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets

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

- [`assetsServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`assetsUploadImage`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`assetsUploadOpenAPIv3`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets
- [`authCallback`](docs/sdks/auth/README.md#callback) - callback auth
- [`authInfo`](docs/sdks/auth/README.md#info) - info auth
- [`authLogin`](docs/sdks/auth/README.md#login) - login auth
- [`authLogout`](docs/sdks/auth/README.md#logout) - logout auth
- [`authSwitchScopes`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`chatList`](docs/sdks/chat/README.md#list) - listChats chat
- [`chatLoad`](docs/sdks/chat/README.md#load) - loadChat chat
- [`deploymentsCreate`](docs/sdks/deployments/README.md#create) - createDeployment deployments
- [`deploymentsEvolveDeployment`](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
- [`deploymentsGetById`](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
- [`deploymentsLatest`](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
- [`deploymentsList`](docs/sdks/deployments/README.md#list) - listDeployments deployments
- [`environmentsCreate`](docs/sdks/environments/README.md#create) - createEnvironment environments
- [`environmentsDeleteBySlug`](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
- [`environmentsList`](docs/sdks/environments/README.md#list) - listEnvironments environments
- [`environmentsUpdateBySlug`](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments
- [`instancesGetBySlug`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`integrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`integrationsList`](docs/sdks/integrations/README.md#list) - list integrations
- [`keysCreate`](docs/sdks/keys/README.md#create) - createKey keys
- [`keysList`](docs/sdks/keys/README.md#list) - listKeys keys
- [`keysRevokeById`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`packagesCreate`](docs/sdks/packages/README.md#create) - createPackage packages
- [`packagesListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`packagesPublish`](docs/sdks/packages/README.md#publish) - publish packages
- [`packagesUpdate`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`projectsCreate`](docs/sdks/projects/README.md#create) - createProject projects
- [`projectsList`](docs/sdks/projects/README.md#list) - listProjects projects
- [`toolsetsCreate`](docs/sdks/toolsets/README.md#create) - createToolset toolsets
- [`toolsetsDeleteBySlug`](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
- [`toolsetsGetBySlug`](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
- [`toolsetsList`](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
- [`toolsetsUpdateBySlug`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`toolsList`](docs/sdks/tools/README.md#list) - listTools tools

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

- [`useCreateAPIKeyMutation`](docs/sdks/keys/README.md#create) - createKey keys
- [`useCreateDeploymentMutation`](docs/sdks/deployments/README.md#create) - createDeployment deployments
- [`useCreateEnvironmentMutation`](docs/sdks/environments/README.md#create) - createEnvironment environments
- [`useCreatePackageMutation`](docs/sdks/packages/README.md#create) - createPackage packages
- [`useCreateProjectMutation`](docs/sdks/projects/README.md#create) - createProject projects
- [`useCreateToolsetMutation`](docs/sdks/toolsets/README.md#create) - createToolset toolsets
- [`useDeleteEnvironmentMutation`](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
- [`useDeleteToolsetMutation`](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
- [`useDeployment`](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
- [`useEvolveDeploymentMutation`](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
- [`useInstance`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`useIntegrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`useLatestDeployment`](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
- [`useListAPIKeys`](docs/sdks/keys/README.md#list) - listKeys keys
- [`useListChats`](docs/sdks/chat/README.md#list) - listChats chat
- [`useListDeployments`](docs/sdks/deployments/README.md#list) - listDeployments deployments
- [`useListEnvironments`](docs/sdks/environments/README.md#list) - listEnvironments environments
- [`useListIntegrations`](docs/sdks/integrations/README.md#list) - list integrations
- [`useListProjects`](docs/sdks/projects/README.md#list) - listProjects projects
- [`useListTools`](docs/sdks/tools/README.md#list) - listTools tools
- [`useListToolsets`](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
- [`useListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`useLoadChat`](docs/sdks/chat/README.md#load) - loadChat chat
- [`useLogoutMutation`](docs/sdks/auth/README.md#logout) - logout auth
- [`usePublishPackageMutation`](docs/sdks/packages/README.md#publish) - publish packages
- [`useRevokeAPIKeyMutation`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`useServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`useSessionInfo`](docs/sdks/auth/README.md#info) - info auth
- [`useSwitchScopesMutation`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`useToolset`](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
- [`useUpdateEnvironmentMutation`](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments
- [`useUpdatePackageMutation`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`useUpdateToolsetMutation`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`useUploadImageMutation`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`useUploadOpenAPIv3Mutation`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets

</details>
<!-- End React hooks with TanStack Query [react-query] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries.  If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API.  However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a retryConfig object to the call:
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.assets.serveImage({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  }, {
    id: "<id>",
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
import { Gram } from "@gram/client";

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
});

async function run() {
  const result = await gram.assets.serveImage({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  }, {
    id: "<id>",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Some methods specify known errors which can be thrown. All the known errors are enumerated in the `models/errors/errors.ts` module. The known errors for a method are documented under the *Errors* tables in SDK docs. For example, the `serveImage` method may throw the following errors:

| Error Type          | Status Code                       | Content Type     |
| ------------------- | --------------------------------- | ---------------- |
| errors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| errors.ServiceError | 500                               | application/json |
| errors.APIError     | 4XX, 5XX                          | \*/\*            |

If the method throws an error and it is not captured by the known errors, it will default to throwing a `APIError`.

```typescript
import { Gram } from "@gram/client";
import { SDKValidationError, ServiceError } from "@gram/client/models/errors";

const gram = new Gram();

async function run() {
  let result;
  try {
    result = await gram.assets.serveImage({
      sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
        ?? "",
    }, {
      id: "<id>",
    });

    // Handle the result
    console.log(result);
  } catch (err) {
    switch (true) {
      // The server response does not match the expected SDK schema
      case (err instanceof SDKValidationError): {
        // Pretty-print will provide a human-readable multi-line error message
        console.error(err.pretty());
        // Raw value may also be inspected
        console.error(err.rawValue);
        return;
      }
      case (err instanceof ServiceError): {
        // Handle err.data$: ServiceErrorData
        console.error(err);
        return;
      }
      case (err instanceof ServiceError): {
        // Handle err.data$: ServiceErrorData
        console.error(err);
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
import { Gram } from "@gram/client";

const gram = new Gram({
  serverURL: "http://localhost:80",
});

async function run() {
  const result = await gram.assets.serveImage({
    sessionHeaderGramSession: process.env["GRAM_SESSION_HEADER_GRAM_SESSION"]
      ?? "",
  }, {
    id: "<id>",
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
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http";

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
import { Gram } from "@gram/client";

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
