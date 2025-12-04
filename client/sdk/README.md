# @gram/client

Developer-friendly & type-safe Typescript SDK specifically catered to leverage *@gram/client* API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=@gram/client&utm_campaign=typescript"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
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
* [@gram/client](#gramclient)
  * [SDK Installation](#sdk-installation)
  * [Requirements](#requirements)
  * [SDK Example Usage](#sdk-example-usage)
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
yarn add <UNSET>
# Install optional peer dependencies if you plan to use React hooks
yarn add @tanstack/react-query react react-dom
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
  const result = await gram.slack.slackLogin({
    projectSlug: "<value>",
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [Agents](docs/sdks/agents/README.md)

* [delete](docs/sdks/agents/README.md#delete) - deleteResponse agents
* [get](docs/sdks/agents/README.md#get) - getResponse agents
* [create](docs/sdks/agents/README.md#create) - createResponse agents

### [Assets](docs/sdks/assets/README.md)

* [fetchOpenAPIv3FromURL](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
* [listAssets](docs/sdks/assets/README.md#listassets) - listAssets assets
* [serveFunction](docs/sdks/assets/README.md#servefunction) - serveFunction assets
* [serveImage](docs/sdks/assets/README.md#serveimage) - serveImage assets
* [serveOpenAPIv3](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
* [uploadFunctions](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
* [uploadImage](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
* [uploadOpenAPIv3](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets

### [Auth](docs/sdks/auth/README.md)

* [callback](docs/sdks/auth/README.md#callback) - callback auth
* [info](docs/sdks/auth/README.md#info) - info auth
* [login](docs/sdks/auth/README.md#login) - login auth
* [logout](docs/sdks/auth/README.md#logout) - logout auth
* [register](docs/sdks/auth/README.md#register) - register auth
* [switchScopes](docs/sdks/auth/README.md#switchscopes) - switchScopes auth

### [Chat](docs/sdks/chat/README.md)

* [creditUsage](docs/sdks/chat/README.md#creditusage) - creditUsage chat
* [list](docs/sdks/chat/README.md#list) - listChats chat
* [load](docs/sdks/chat/README.md#load) - loadChat chat

### [Deployments](docs/sdks/deployments/README.md)

* [active](docs/sdks/deployments/README.md#active) - getActiveDeployment deployments
* [create](docs/sdks/deployments/README.md#create) - createDeployment deployments
* [evolveDeployment](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
* [getById](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
* [latest](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
* [list](docs/sdks/deployments/README.md#list) - listDeployments deployments
* [logs](docs/sdks/deployments/README.md#logs) - getDeploymentLogs deployments
* [redeployDeployment](docs/sdks/deployments/README.md#redeploydeployment) - redeploy deployments

### [Domains](docs/sdks/domains/README.md)

* [deleteDomain](docs/sdks/domains/README.md#deletedomain) - deleteDomain domains
* [getDomain](docs/sdks/domains/README.md#getdomain) - getDomain domains
* [registerDomain](docs/sdks/domains/README.md#registerdomain) - createDomain domains

### [Environments](docs/sdks/environments/README.md)

* [create](docs/sdks/environments/README.md#create) - createEnvironment environments
* [deleteBySlug](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
* [deleteSourceLink](docs/sdks/environments/README.md#deletesourcelink) - deleteSourceEnvironmentLink environments
* [deleteToolsetLink](docs/sdks/environments/README.md#deletetoolsetlink) - deleteToolsetEnvironmentLink environments
* [getBySource](docs/sdks/environments/README.md#getbysource) - getSourceEnvironment environments
* [getByToolset](docs/sdks/environments/README.md#getbytoolset) - getToolsetEnvironment environments
* [list](docs/sdks/environments/README.md#list) - listEnvironments environments
* [setSourceLink](docs/sdks/environments/README.md#setsourcelink) - setSourceEnvironmentLink environments
* [setToolsetLink](docs/sdks/environments/README.md#settoolsetlink) - setToolsetEnvironmentLink environments
* [updateBySlug](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments

### [Features](docs/sdks/features/README.md)

* [set](docs/sdks/features/README.md#set) - setProductFeature features

### [Instances](docs/sdks/instances/README.md)

* [getBySlug](docs/sdks/instances/README.md#getbyslug) - getInstance instances

### [Integrations](docs/sdks/integrations/README.md)

* [integrationsNumberGet](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
* [list](docs/sdks/integrations/README.md#list) - list integrations

### [Keys](docs/sdks/keys/README.md)

* [create](docs/sdks/keys/README.md#create) - createKey keys
* [list](docs/sdks/keys/README.md#list) - listKeys keys
* [revokeById](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
* [validate](docs/sdks/keys/README.md#validate) - verifyKey keys

### [Logs](docs/sdks/logs/README.md)

* [list](docs/sdks/logs/README.md#list) - listLogs logs
* [listToolExecutionLogs](docs/sdks/logs/README.md#listtoolexecutionlogs) - listToolExecutionLogs logs

### [McpMetadata](docs/sdks/mcpmetadata/README.md)

* [get](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
* [set](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata

### [McpRegistries](docs/sdks/mcpregistries/README.md)

* [listCatalog](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries

### [Packages](docs/sdks/packages/README.md)

* [create](docs/sdks/packages/README.md#create) - createPackage packages
* [list](docs/sdks/packages/README.md#list) - listPackages packages
* [listVersions](docs/sdks/packages/README.md#listversions) - listVersions packages
* [publish](docs/sdks/packages/README.md#publish) - publish packages
* [update](docs/sdks/packages/README.md#update) - updatePackage packages

### [Projects](docs/sdks/projects/README.md)

* [create](docs/sdks/projects/README.md#create) - createProject projects
* [list](docs/sdks/projects/README.md#list) - listProjects projects
* [setLogo](docs/sdks/projects/README.md#setlogo) - setLogo projects

### [Resources](docs/sdks/resources/README.md)

* [list](docs/sdks/resources/README.md#list) - listResources resources

### [Slack](docs/sdks/slack/README.md)

* [slackLogin](docs/sdks/slack/README.md#slacklogin) - login slack
* [slackCallback](docs/sdks/slack/README.md#slackcallback) - callback slack
* [deleteSlackConnection](docs/sdks/slack/README.md#deleteslackconnection) - deleteSlackConnection slack
* [getSlackConnection](docs/sdks/slack/README.md#getslackconnection) - getSlackConnection slack
* [updateSlackConnection](docs/sdks/slack/README.md#updateslackconnection) - updateSlackConnection slack

### [Templates](docs/sdks/templates/README.md)

* [create](docs/sdks/templates/README.md#create) - createTemplate templates
* [delete](docs/sdks/templates/README.md#delete) - deleteTemplate templates
* [get](docs/sdks/templates/README.md#get) - getTemplate templates
* [list](docs/sdks/templates/README.md#list) - listTemplates templates
* [renderByID](docs/sdks/templates/README.md#renderbyid) - renderTemplateByID templates
* [render](docs/sdks/templates/README.md#render) - renderTemplate templates
* [update](docs/sdks/templates/README.md#update) - updateTemplate templates

### [Tools](docs/sdks/tools/README.md)

* [list](docs/sdks/tools/README.md#list) - listTools tools

### [Toolsets](docs/sdks/toolsets/README.md)

* [addExternalOAuthServer](docs/sdks/toolsets/README.md#addexternaloauthserver) - addExternalOAuthServer toolsets
* [addOAuthProxyServer](docs/sdks/toolsets/README.md#addoauthproxyserver) - addOAuthProxyServer toolsets
* [checkMCPSlugAvailability](docs/sdks/toolsets/README.md#checkmcpslugavailability) - checkMCPSlugAvailability toolsets
* [cloneBySlug](docs/sdks/toolsets/README.md#clonebyslug) - cloneToolset toolsets
* [create](docs/sdks/toolsets/README.md#create) - createToolset toolsets
* [deleteBySlug](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
* [getBySlug](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
* [list](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
* [removeOAuthServer](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
* [updateBySlug](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets

### [Usage](docs/sdks/usage/README.md)

* [createCheckout](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
* [createCustomerSession](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
* [getPeriodUsage](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
* [getUsageTiers](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage

### [Variations](docs/sdks/variations/README.md)

* [deleteGlobal](docs/sdks/variations/README.md#deleteglobal) - deleteGlobal variations
* [listGlobal](docs/sdks/variations/README.md#listglobal) - listGlobal variations
* [upsertGlobal](docs/sdks/variations/README.md#upsertglobal) - upsertGlobal variations

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

- [`agentsCreate`](docs/sdks/agents/README.md#create) - createResponse agents
- [`agentsDelete`](docs/sdks/agents/README.md#delete) - deleteResponse agents
- [`agentsGet`](docs/sdks/agents/README.md#get) - getResponse agents
- [`assetsFetchOpenAPIv3FromURL`](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
- [`assetsListAssets`](docs/sdks/assets/README.md#listassets) - listAssets assets
- [`assetsServeFunction`](docs/sdks/assets/README.md#servefunction) - serveFunction assets
- [`assetsServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`assetsServeOpenAPIv3`](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
- [`assetsUploadFunctions`](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
- [`assetsUploadImage`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`assetsUploadOpenAPIv3`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets
- [`authCallback`](docs/sdks/auth/README.md#callback) - callback auth
- [`authInfo`](docs/sdks/auth/README.md#info) - info auth
- [`authLogin`](docs/sdks/auth/README.md#login) - login auth
- [`authLogout`](docs/sdks/auth/README.md#logout) - logout auth
- [`authRegister`](docs/sdks/auth/README.md#register) - register auth
- [`authSwitchScopes`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`chatCreditUsage`](docs/sdks/chat/README.md#creditusage) - creditUsage chat
- [`chatList`](docs/sdks/chat/README.md#list) - listChats chat
- [`chatLoad`](docs/sdks/chat/README.md#load) - loadChat chat
- [`deploymentsActive`](docs/sdks/deployments/README.md#active) - getActiveDeployment deployments
- [`deploymentsCreate`](docs/sdks/deployments/README.md#create) - createDeployment deployments
- [`deploymentsEvolveDeployment`](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
- [`deploymentsGetById`](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
- [`deploymentsLatest`](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
- [`deploymentsList`](docs/sdks/deployments/README.md#list) - listDeployments deployments
- [`deploymentsLogs`](docs/sdks/deployments/README.md#logs) - getDeploymentLogs deployments
- [`deploymentsRedeployDeployment`](docs/sdks/deployments/README.md#redeploydeployment) - redeploy deployments
- [`domainsDeleteDomain`](docs/sdks/domains/README.md#deletedomain) - deleteDomain domains
- [`domainsGetDomain`](docs/sdks/domains/README.md#getdomain) - getDomain domains
- [`domainsRegisterDomain`](docs/sdks/domains/README.md#registerdomain) - createDomain domains
- [`environmentsCreate`](docs/sdks/environments/README.md#create) - createEnvironment environments
- [`environmentsDeleteBySlug`](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
- [`environmentsDeleteSourceLink`](docs/sdks/environments/README.md#deletesourcelink) - deleteSourceEnvironmentLink environments
- [`environmentsDeleteToolsetLink`](docs/sdks/environments/README.md#deletetoolsetlink) - deleteToolsetEnvironmentLink environments
- [`environmentsGetBySource`](docs/sdks/environments/README.md#getbysource) - getSourceEnvironment environments
- [`environmentsGetByToolset`](docs/sdks/environments/README.md#getbytoolset) - getToolsetEnvironment environments
- [`environmentsList`](docs/sdks/environments/README.md#list) - listEnvironments environments
- [`environmentsSetSourceLink`](docs/sdks/environments/README.md#setsourcelink) - setSourceEnvironmentLink environments
- [`environmentsSetToolsetLink`](docs/sdks/environments/README.md#settoolsetlink) - setToolsetEnvironmentLink environments
- [`environmentsUpdateBySlug`](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments
- [`featuresSet`](docs/sdks/features/README.md#set) - setProductFeature features
- [`instancesGetBySlug`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`integrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`integrationsList`](docs/sdks/integrations/README.md#list) - list integrations
- [`keysCreate`](docs/sdks/keys/README.md#create) - createKey keys
- [`keysList`](docs/sdks/keys/README.md#list) - listKeys keys
- [`keysRevokeById`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`keysValidate`](docs/sdks/keys/README.md#validate) - verifyKey keys
- [`logsList`](docs/sdks/logs/README.md#list) - listLogs logs
- [`logsListToolExecutionLogs`](docs/sdks/logs/README.md#listtoolexecutionlogs) - listToolExecutionLogs logs
- [`mcpMetadataGet`](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
- [`mcpMetadataSet`](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata
- [`mcpRegistriesListCatalog`](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries
- [`packagesCreate`](docs/sdks/packages/README.md#create) - createPackage packages
- [`packagesList`](docs/sdks/packages/README.md#list) - listPackages packages
- [`packagesListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`packagesPublish`](docs/sdks/packages/README.md#publish) - publish packages
- [`packagesUpdate`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`projectsCreate`](docs/sdks/projects/README.md#create) - createProject projects
- [`projectsList`](docs/sdks/projects/README.md#list) - listProjects projects
- [`projectsSetLogo`](docs/sdks/projects/README.md#setlogo) - setLogo projects
- [`resourcesList`](docs/sdks/resources/README.md#list) - listResources resources
- [`slackDeleteSlackConnection`](docs/sdks/slack/README.md#deleteslackconnection) - deleteSlackConnection slack
- [`slackGetSlackConnection`](docs/sdks/slack/README.md#getslackconnection) - getSlackConnection slack
- [`slackSlackCallback`](docs/sdks/slack/README.md#slackcallback) - callback slack
- [`slackSlackLogin`](docs/sdks/slack/README.md#slacklogin) - login slack
- [`slackUpdateSlackConnection`](docs/sdks/slack/README.md#updateslackconnection) - updateSlackConnection slack
- [`templatesCreate`](docs/sdks/templates/README.md#create) - createTemplate templates
- [`templatesDelete`](docs/sdks/templates/README.md#delete) - deleteTemplate templates
- [`templatesGet`](docs/sdks/templates/README.md#get) - getTemplate templates
- [`templatesList`](docs/sdks/templates/README.md#list) - listTemplates templates
- [`templatesRender`](docs/sdks/templates/README.md#render) - renderTemplate templates
- [`templatesRenderByID`](docs/sdks/templates/README.md#renderbyid) - renderTemplateByID templates
- [`templatesUpdate`](docs/sdks/templates/README.md#update) - updateTemplate templates
- [`toolsetsAddExternalOAuthServer`](docs/sdks/toolsets/README.md#addexternaloauthserver) - addExternalOAuthServer toolsets
- [`toolsetsAddOAuthProxyServer`](docs/sdks/toolsets/README.md#addoauthproxyserver) - addOAuthProxyServer toolsets
- [`toolsetsCheckMCPSlugAvailability`](docs/sdks/toolsets/README.md#checkmcpslugavailability) - checkMCPSlugAvailability toolsets
- [`toolsetsCloneBySlug`](docs/sdks/toolsets/README.md#clonebyslug) - cloneToolset toolsets
- [`toolsetsCreate`](docs/sdks/toolsets/README.md#create) - createToolset toolsets
- [`toolsetsDeleteBySlug`](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
- [`toolsetsGetBySlug`](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
- [`toolsetsList`](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
- [`toolsetsRemoveOAuthServer`](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
- [`toolsetsUpdateBySlug`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`toolsList`](docs/sdks/tools/README.md#list) - listTools tools
- [`usageCreateCheckout`](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
- [`usageCreateCustomerSession`](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
- [`usageGetPeriodUsage`](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
- [`usageGetUsageTiers`](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage
- [`variationsDeleteGlobal`](docs/sdks/variations/README.md#deleteglobal) - deleteGlobal variations
- [`variationsListGlobal`](docs/sdks/variations/README.md#listglobal) - listGlobal variations
- [`variationsUpsertGlobal`](docs/sdks/variations/README.md#upsertglobal) - upsertGlobal variations

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

- [`useActiveDeployment`](docs/sdks/deployments/README.md#active) - getActiveDeployment deployments
- [`useAddExternalOAuthServerMutation`](docs/sdks/toolsets/README.md#addexternaloauthserver) - addExternalOAuthServer toolsets
- [`useAddOAuthProxyServerMutation`](docs/sdks/toolsets/README.md#addoauthproxyserver) - addOAuthProxyServer toolsets
- [`useAgentsCreateMutation`](docs/sdks/agents/README.md#create) - createResponse agents
- [`useAgentsDeleteMutation`](docs/sdks/agents/README.md#delete) - deleteResponse agents
- [`useAgentsGet`](docs/sdks/agents/README.md#get) - getResponse agents
- [`useCheckMCPSlugAvailability`](docs/sdks/toolsets/README.md#checkmcpslugavailability) - checkMCPSlugAvailability toolsets
- [`useCloneToolsetMutation`](docs/sdks/toolsets/README.md#clonebyslug) - cloneToolset toolsets
- [`useCreateAPIKeyMutation`](docs/sdks/keys/README.md#create) - createKey keys
- [`useCreateCheckoutMutation`](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
- [`useCreateCustomerSessionMutation`](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
- [`useCreateDeploymentMutation`](docs/sdks/deployments/README.md#create) - createDeployment deployments
- [`useCreateEnvironmentMutation`](docs/sdks/environments/README.md#create) - createEnvironment environments
- [`useCreatePackageMutation`](docs/sdks/packages/README.md#create) - createPackage packages
- [`useCreateProjectMutation`](docs/sdks/projects/README.md#create) - createProject projects
- [`useCreateTemplateMutation`](docs/sdks/templates/README.md#create) - createTemplate templates
- [`useCreateToolsetMutation`](docs/sdks/toolsets/README.md#create) - createToolset toolsets
- [`useDeleteDomainMutation`](docs/sdks/domains/README.md#deletedomain) - deleteDomain domains
- [`useDeleteEnvironmentMutation`](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
- [`useDeleteGlobalVariationMutation`](docs/sdks/variations/README.md#deleteglobal) - deleteGlobal variations
- [`useDeleteSlackConnectionMutation`](docs/sdks/slack/README.md#deleteslackconnection) - deleteSlackConnection slack
- [`useDeleteSourceEnvironmentLinkMutation`](docs/sdks/environments/README.md#deletesourcelink) - deleteSourceEnvironmentLink environments
- [`useDeleteTemplateMutation`](docs/sdks/templates/README.md#delete) - deleteTemplate templates
- [`useDeleteToolsetEnvironmentLinkMutation`](docs/sdks/environments/README.md#deletetoolsetlink) - deleteToolsetEnvironmentLink environments
- [`useDeleteToolsetMutation`](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
- [`useDeployment`](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
- [`useDeploymentLogs`](docs/sdks/deployments/README.md#logs) - getDeploymentLogs deployments
- [`useEvolveDeploymentMutation`](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
- [`useFeaturesSetMutation`](docs/sdks/features/README.md#set) - setProductFeature features
- [`useFetchOpenAPIv3FromURLMutation`](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
- [`useGetCreditUsage`](docs/sdks/chat/README.md#creditusage) - creditUsage chat
- [`useGetDomain`](docs/sdks/domains/README.md#getdomain) - getDomain domains
- [`useGetMcpMetadata`](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
- [`useGetPeriodUsage`](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
- [`useGetSlackConnection`](docs/sdks/slack/README.md#getslackconnection) - getSlackConnection slack
- [`useGetSourceEnvironment`](docs/sdks/environments/README.md#getbysource) - getSourceEnvironment environments
- [`useGetToolsetEnvironment`](docs/sdks/environments/README.md#getbytoolset) - getToolsetEnvironment environments
- [`useGetUsageTiers`](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage
- [`useGlobalVariations`](docs/sdks/variations/README.md#listglobal) - listGlobal variations
- [`useInstance`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`useIntegrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`useLatestDeployment`](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
- [`useListAPIKeys`](docs/sdks/keys/README.md#list) - listKeys keys
- [`useListAssets`](docs/sdks/assets/README.md#listassets) - listAssets assets
- [`useListChats`](docs/sdks/chat/README.md#list) - listChats chat
- [`useListDeployments`](docs/sdks/deployments/README.md#list) - listDeployments deployments
- [`useListEnvironments`](docs/sdks/environments/README.md#list) - listEnvironments environments
- [`useListIntegrations`](docs/sdks/integrations/README.md#list) - list integrations
- [`useListMcpCatalog`](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries
- [`useListPackages`](docs/sdks/packages/README.md#list) - listPackages packages
- [`useListProjects`](docs/sdks/projects/README.md#list) - listProjects projects
- [`useListResources`](docs/sdks/resources/README.md#list) - listResources resources
- [`useListToolLogs`](docs/sdks/logs/README.md#list) - listLogs logs
- [`useListTools`](docs/sdks/tools/README.md#list) - listTools tools
- [`useListToolsets`](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
- [`useListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`useLoadChat`](docs/sdks/chat/README.md#load) - loadChat chat
- [`useLogoutMutation`](docs/sdks/auth/README.md#logout) - logout auth
- [`useMcpMetadataSetMutation`](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata
- [`usePublishPackageMutation`](docs/sdks/packages/README.md#publish) - publish packages
- [`useRedeployDeploymentMutation`](docs/sdks/deployments/README.md#redeploydeployment) - redeploy deployments
- [`useRegisterDomainMutation`](docs/sdks/domains/README.md#registerdomain) - createDomain domains
- [`useRegisterMutation`](docs/sdks/auth/README.md#register) - register auth
- [`useRemoveOAuthServerMutation`](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
- [`useRenderTemplate`](docs/sdks/templates/README.md#render) - renderTemplate templates
- [`useRenderTemplateByID`](docs/sdks/templates/README.md#renderbyid) - renderTemplateByID templates
- [`useRevokeAPIKeyMutation`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`useServeFunction`](docs/sdks/assets/README.md#servefunction) - serveFunction assets
- [`useServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`useServeOpenAPIv3`](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
- [`useSessionInfo`](docs/sdks/auth/README.md#info) - info auth
- [`useSetProjectLogoMutation`](docs/sdks/projects/README.md#setlogo) - setLogo projects
- [`useSetSourceEnvironmentLinkMutation`](docs/sdks/environments/README.md#setsourcelink) - setSourceEnvironmentLink environments
- [`useSetToolsetEnvironmentLinkMutation`](docs/sdks/environments/README.md#settoolsetlink) - setToolsetEnvironmentLink environments
- [`useSwitchScopesMutation`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`useTemplate`](docs/sdks/templates/README.md#get) - getTemplate templates
- [`useTemplates`](docs/sdks/templates/README.md#list) - listTemplates templates
- [`useToolExecutionLogs`](docs/sdks/logs/README.md#listtoolexecutionlogs) - listToolExecutionLogs logs
- [`useToolset`](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
- [`useUpdateEnvironmentMutation`](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments
- [`useUpdatePackageMutation`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`useUpdateSlackConnectionMutation`](docs/sdks/slack/README.md#updateslackconnection) - updateSlackConnection slack
- [`useUpdateTemplateMutation`](docs/sdks/templates/README.md#update) - updateTemplate templates
- [`useUpdateToolsetMutation`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`useUploadFunctionsMutation`](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
- [`useUploadImageMutation`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`useUploadOpenAPIv3Mutation`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets
- [`useUpsertGlobalVariationMutation`](docs/sdks/variations/README.md#upsertglobal) - upsertGlobal variations
- [`useValidateAPIKey`](docs/sdks/keys/README.md#validate) - verifyKey keys

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
  const result = await gram.slack.slackLogin({
    projectSlug: "<value>",
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
  const result = await gram.slack.slackLogin({
    projectSlug: "<value>",
  });

  console.log(result);
}

run();

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

[`GramError`](./src/models/errors/gramerror.ts) is the base class for all HTTP error responses. It has the following properties:

| Property            | Type       | Description                                                                             |
| ------------------- | ---------- | --------------------------------------------------------------------------------------- |
| `error.message`     | `string`   | Error message                                                                           |
| `error.statusCode`  | `number`   | HTTP response status code eg `404`                                                      |
| `error.headers`     | `Headers`  | HTTP response headers                                                                   |
| `error.body`        | `string`   | HTTP body. Can be empty string if no body is returned.                                  |
| `error.rawResponse` | `Response` | Raw HTTP response                                                                       |
| `error.data$`       |            | Optional. Some errors may contain structured data. [See Error Classes](#error-classes). |

### Example
```typescript
import { Gram } from "@gram/client";
import * as errors from "@gram/client/models/errors";

const gram = new Gram();

async function run() {
  try {
    const result = await gram.slack.slackLogin({
      projectSlug: "<value>",
    });

    console.log(result);
  } catch (error) {
    // The base class for HTTP error responses
    if (error instanceof errors.GramError) {
      console.log(error.message);
      console.log(error.statusCode);
      console.log(error.body);
      console.log(error.headers);

      // Depending on the method different errors may be thrown
      if (error instanceof errors.ServiceError) {
        console.log(error.data$.fault); // boolean
        console.log(error.data$.id); // string
        console.log(error.data$.message); // string
        console.log(error.data$.name); // string
        console.log(error.data$.temporary); // boolean
      }
    }
  }
}

run();

```

### Error Classes
**Primary errors:**
* [`GramError`](./src/models/errors/gramerror.ts): The base class for HTTP error responses.
  * [`ServiceError`](./src/models/errors/serviceerror.ts): unauthorized access.

<details><summary>Less common errors (6)</summary>

<br />

**Network errors:**
* [`ConnectionError`](./src/models/errors/httpclienterrors.ts): HTTP client was unable to make a request to a server.
* [`RequestTimeoutError`](./src/models/errors/httpclienterrors.ts): HTTP request timed out due to an AbortSignal signal.
* [`RequestAbortedError`](./src/models/errors/httpclienterrors.ts): HTTP request was aborted by the client.
* [`InvalidRequestError`](./src/models/errors/httpclienterrors.ts): Any input used to create a request is invalid.
* [`UnexpectedClientError`](./src/models/errors/httpclienterrors.ts): Unrecognised or unexpected error.


**Inherit from [`GramError`](./src/models/errors/gramerror.ts)**:
* [`ResponseValidationError`](./src/models/errors/responsevalidationerror.ts): Type mismatch between the data returned from the server and the structure expected by the SDK. See `error.rawValue` for the raw value and `error.pretty()` for a nicely formatted multi-line string.

</details>
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
  const result = await gram.slack.slackLogin({
    projectSlug: "<value>",
  });

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

const sdk = new Gram({ httpClient: httpClient });
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

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=@gram/client&utm_campaign=typescript)
