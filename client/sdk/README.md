# @gram/client

Developer-friendly & type-safe Typescript SDK specifically catered to leverage _@gram/client_ API.

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
  * [Pagination](#pagination)
  * [File uploads](#file-uploads)
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
  const result = await gram.access.createRole({
    createRoleForm: {
      description: "swerve hm receptor how",
      grants: [
        {
          scope: "mcp:connect",
        },
      ],
      name: "<value>",
    },
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

### [Access](docs/sdks/access/README.md)

* [createRole](docs/sdks/access/README.md#createrole) - createRole access
* [deleteRole](docs/sdks/access/README.md#deleterole) - deleteRole access
* [disableRBAC](docs/sdks/access/README.md#disablerbac) - disableRBAC access
* [enableRBAC](docs/sdks/access/README.md#enablerbac) - enableRBAC access
* [getRBACStatus](docs/sdks/access/README.md#getrbacstatus) - getRBACStatus access
* [getRole](docs/sdks/access/README.md#getrole) - getRole access
* [listChallenges](docs/sdks/access/README.md#listchallenges) - listChallenges access
* [listGrants](docs/sdks/access/README.md#listgrants) - listGrants access
* [listMembers](docs/sdks/access/README.md#listmembers) - listMembers access
* [listRoles](docs/sdks/access/README.md#listroles) - listRoles access
* [listScopes](docs/sdks/access/README.md#listscopes) - listScopes access
* [resolveChallenge](docs/sdks/access/README.md#resolvechallenge) - resolveChallenge access
* [updateMemberRole](docs/sdks/access/README.md#updatememberrole) - updateMemberRole access
* [updateRole](docs/sdks/access/README.md#updaterole) - updateRole access

### [Assets](docs/sdks/assets/README.md)

* [createSignedChatAttachmentURL](docs/sdks/assets/README.md#createsignedchatattachmenturl) - createSignedChatAttachmentURL assets
* [fetchOpenAPIv3FromURL](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
* [listAssets](docs/sdks/assets/README.md#listassets) - listAssets assets
* [serveChatAttachment](docs/sdks/assets/README.md#servechatattachment) - serveChatAttachment assets
* [serveChatAttachmentSigned](docs/sdks/assets/README.md#servechatattachmentsigned) - serveChatAttachmentSigned assets
* [serveFunction](docs/sdks/assets/README.md#servefunction) - serveFunction assets
* [serveImage](docs/sdks/assets/README.md#serveimage) - serveImage assets
* [serveOpenAPIv3](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
* [uploadChatAttachment](docs/sdks/assets/README.md#uploadchatattachment) - uploadChatAttachment assets
* [uploadFunctions](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
* [uploadImage](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
* [uploadOpenAPIv3](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets

### [Assistants](docs/sdks/assistants/README.md)

* [create](docs/sdks/assistants/README.md#create) - createAssistant assistants
* [delete](docs/sdks/assistants/README.md#delete) - deleteAssistant assistants
* [get](docs/sdks/assistants/README.md#get) - getAssistant assistants
* [list](docs/sdks/assistants/README.md#list) - listAssistants assistants
* [update](docs/sdks/assistants/README.md#update) - updateAssistant assistants

### [Auditlogs](docs/sdks/auditlogs/README.md)

* [list](docs/sdks/auditlogs/README.md#list) - list auditlogs
* [listFacets](docs/sdks/auditlogs/README.md#listfacets) - listFacets auditlogs

### [Auth](docs/sdks/auth/README.md)

* [callback](docs/sdks/auth/README.md#callback) - callback auth
* [info](docs/sdks/auth/README.md#info) - info auth
* [login](docs/sdks/auth/README.md#login) - login auth
* [logout](docs/sdks/auth/README.md#logout) - logout auth
* [register](docs/sdks/auth/README.md#register) - register auth
* [switchScopes](docs/sdks/auth/README.md#switchscopes) - switchScopes auth

### [Chat](docs/sdks/chat/README.md)

* [creditUsage](docs/sdks/chat/README.md#creditusage) - creditUsage chat
* [delete](docs/sdks/chat/README.md#delete) - deleteChat chat
* [generateTitle](docs/sdks/chat/README.md#generatetitle) - generateTitle chat
* [list](docs/sdks/chat/README.md#list) - listChats chat
* [listChatsWithResolutions](docs/sdks/chat/README.md#listchatswithresolutions) - listChatsWithResolutions chat
* [load](docs/sdks/chat/README.md#load) - loadChat chat
* [submitFeedback](docs/sdks/chat/README.md#submitfeedback) - submitFeedback chat

### [ChatSessions](docs/sdks/chatsessions/README.md)

* [create](docs/sdks/chatsessions/README.md#create) - create chatSessions
* [revoke](docs/sdks/chatsessions/README.md#revoke) - revoke chatSessions

### [Collections](docs/sdks/collections/README.md)

* [attachServer](docs/sdks/collections/README.md#attachserver) - attachServer collections
* [create](docs/sdks/collections/README.md#create) - create collections
* [delete](docs/sdks/collections/README.md#delete) - delete collections
* [detachServer](docs/sdks/collections/README.md#detachserver) - detachServer collections
* [list](docs/sdks/collections/README.md#list) - list collections
* [listServers](docs/sdks/collections/README.md#listservers) - listServers collections
* [update](docs/sdks/collections/README.md#update) - update collections

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

* [get](docs/sdks/features/README.md#get) - getProductFeatures features
* [set](docs/sdks/features/README.md#set) - setProductFeature features

### [Hooks](docs/sdks/hooks/README.md)

* [hooksNumberClaude](docs/sdks/hooks/README.md#hooksnumberclaude) - claude hooks
* [hooksNumberCursor](docs/sdks/hooks/README.md#hooksnumbercursor) - cursor hooks
* [hooksNumberLogs](docs/sdks/hooks/README.md#hooksnumberlogs) - logs hooks
* [hooksNumberMetrics](docs/sdks/hooks/README.md#hooksnumbermetrics) - metrics hooks

### [HooksServerNames](docs/sdks/hooksservernames/README.md)

* [deleteServerNameOverride](docs/sdks/hooksservernames/README.md#deleteservernameoverride) - delete hooksServerNames
* [listServerNameOverrides](docs/sdks/hooksservernames/README.md#listservernameoverrides) - list hooksServerNames
* [upsertServerNameOverride](docs/sdks/hooksservernames/README.md#upsertservernameoverride) - upsert hooksServerNames

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

### [McpEndpoints](docs/sdks/mcpendpoints/README.md)

* [create](docs/sdks/mcpendpoints/README.md#create) - createMcpEndpoint mcpEndpoints
* [delete](docs/sdks/mcpendpoints/README.md#delete) - deleteMcpEndpoint mcpEndpoints
* [get](docs/sdks/mcpendpoints/README.md#get) - getMcpEndpoint mcpEndpoints
* [list](docs/sdks/mcpendpoints/README.md#list) - listMcpEndpoints mcpEndpoints
* [update](docs/sdks/mcpendpoints/README.md#update) - updateMcpEndpoint mcpEndpoints

### [McpMetadata](docs/sdks/mcpmetadata/README.md)

* [export](docs/sdks/mcpmetadata/README.md#export) - exportMcpMetadata mcpMetadata
* [get](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
* [set](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata

### [McpRegistries](docs/sdks/mcpregistries/README.md)

* [clearCache](docs/sdks/mcpregistries/README.md#clearcache) - clearCache mcpRegistries
* [getServerDetails](docs/sdks/mcpregistries/README.md#getserverdetails) - getServerDetails mcpRegistries
* [listCatalog](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries
* [listRegistries](docs/sdks/mcpregistries/README.md#listregistries) - listRegistries mcpRegistries

### [McpServers](docs/sdks/mcpservers/README.md)

* [create](docs/sdks/mcpservers/README.md#create) - createMcpServer mcpServers
* [delete](docs/sdks/mcpservers/README.md#delete) - deleteMcpServer mcpServers
* [get](docs/sdks/mcpservers/README.md#get) - getMcpServer mcpServers
* [list](docs/sdks/mcpservers/README.md#list) - listMcpServers mcpServers
* [update](docs/sdks/mcpservers/README.md#update) - updateMcpServer mcpServers

### [Organizations](docs/sdks/organizations/README.md)

* [getInviteByToken](docs/sdks/organizations/README.md#getinvitebytoken) - getInviteByToken organizations
* [listInvites](docs/sdks/organizations/README.md#listinvites) - listInvites organizations
* [listUsers](docs/sdks/organizations/README.md#listusers) - listUsers organizations
* [removeUser](docs/sdks/organizations/README.md#removeuser) - removeUser organizations
* [revokeInvite](docs/sdks/organizations/README.md#revokeinvite) - revokeInvite organizations
* [sendInvite](docs/sdks/organizations/README.md#sendinvite) - sendInvite organizations

### [Packages](docs/sdks/packages/README.md)

* [create](docs/sdks/packages/README.md#create) - createPackage packages
* [list](docs/sdks/packages/README.md#list) - listPackages packages
* [listVersions](docs/sdks/packages/README.md#listversions) - listVersions packages
* [publish](docs/sdks/packages/README.md#publish) - publish packages
* [update](docs/sdks/packages/README.md#update) - updatePackage packages

### [Plugins](docs/sdks/plugins/README.md)

* [addPluginServer](docs/sdks/plugins/README.md#addpluginserver) - addPluginServer plugins
* [createPlugin](docs/sdks/plugins/README.md#createplugin) - createPlugin plugins
* [deletePlugin](docs/sdks/plugins/README.md#deleteplugin) - deletePlugin plugins
* [downloadObservabilityPlugin](docs/sdks/plugins/README.md#downloadobservabilityplugin) - downloadObservabilityPlugin plugins
* [downloadPluginPackage](docs/sdks/plugins/README.md#downloadpluginpackage) - downloadPluginPackage plugins
* [getPlugin](docs/sdks/plugins/README.md#getplugin) - getPlugin plugins
* [getPublishStatus](docs/sdks/plugins/README.md#getpublishstatus) - getPublishStatus plugins
* [listPlugins](docs/sdks/plugins/README.md#listplugins) - listPlugins plugins
* [publishPlugins](docs/sdks/plugins/README.md#publishplugins) - publishPlugins plugins
* [removePluginServer](docs/sdks/plugins/README.md#removepluginserver) - removePluginServer plugins
* [setPluginAssignments](docs/sdks/plugins/README.md#setpluginassignments) - setPluginAssignments plugins
* [updatePlugin](docs/sdks/plugins/README.md#updateplugin) - updatePlugin plugins
* [updatePluginServer](docs/sdks/plugins/README.md#updatepluginserver) - updatePluginServer plugins

### [Projects](docs/sdks/projects/README.md)

* [create](docs/sdks/projects/README.md#create) - createProject projects
* [deleteById](docs/sdks/projects/README.md#deletebyid) - deleteProject projects
* [read](docs/sdks/projects/README.md#read) - getProject projects
* [list](docs/sdks/projects/README.md#list) - listProjects projects
* [listAllowedOrigins](docs/sdks/projects/README.md#listallowedorigins) - listAllowedOrigins projects
* [setLogo](docs/sdks/projects/README.md#setlogo) - setLogo projects
* [setOrganizationWhitelist](docs/sdks/projects/README.md#setorganizationwhitelist) - setOrganizationWhitelist projects
* [upsertAllowedOrigin](docs/sdks/projects/README.md#upsertallowedorigin) - upsertAllowedOrigin projects

### [RemoteMcp](docs/sdks/remotemcp/README.md)

* [createServer](docs/sdks/remotemcp/README.md#createserver) - createServer remoteMcp
* [deleteServer](docs/sdks/remotemcp/README.md#deleteserver) - deleteServer remoteMcp
* [getServer](docs/sdks/remotemcp/README.md#getserver) - getServer remoteMcp
* [listServers](docs/sdks/remotemcp/README.md#listservers) - listServers remoteMcp
* [updateServer](docs/sdks/remotemcp/README.md#updateserver) - updateServer remoteMcp

### [Resources](docs/sdks/resources/README.md)

* [list](docs/sdks/resources/README.md#list) - listResources resources

### [Risk.Policies](docs/sdks/policies/README.md)

* [create](docs/sdks/policies/README.md#create) - createRiskPolicy risk
* [delete](docs/sdks/policies/README.md#delete) - deleteRiskPolicy risk
* [get](docs/sdks/policies/README.md#get) - getRiskPolicy risk
* [list](docs/sdks/policies/README.md#list) - listRiskPolicies risk
* [status](docs/sdks/policies/README.md#status) - getRiskPolicyStatus risk
* [trigger](docs/sdks/policies/README.md#trigger) - triggerRiskAnalysis risk
* [update](docs/sdks/policies/README.md#update) - updateRiskPolicy risk

### [Risk.Results](docs/sdks/results/README.md)

* [byChat](docs/sdks/results/README.md#bychat) - listRiskResultsByChat risk
* [list](docs/sdks/results/README.md#list) - listRiskResults risk

### [Slack](docs/sdks/slack/README.md)

* [configureSlackApp](docs/sdks/slack/README.md#configureslackapp) - configureSlackApp slack
* [createSlackApp](docs/sdks/slack/README.md#createslackapp) - createSlackApp slack
* [deleteSlackApp](docs/sdks/slack/README.md#deleteslackapp) - deleteSlackApp slack
* [getSlackApp](docs/sdks/slack/README.md#getslackapp) - getSlackApp slack
* [listSlackApps](docs/sdks/slack/README.md#listslackapps) - listSlackApps slack
* [updateSlackApp](docs/sdks/slack/README.md#updateslackapp) - updateSlackApp slack

### [Telemetry](docs/sdks/telemetry/README.md)

* [captureEvent](docs/sdks/telemetry/README.md#captureevent) - captureEvent telemetry
* [getHooksSummary](docs/sdks/telemetry/README.md#gethookssummary) - getHooksSummary telemetry
* [getObservabilityOverview](docs/sdks/telemetry/README.md#getobservabilityoverview) - getObservabilityOverview telemetry
* [getProjectMetricsSummary](docs/sdks/telemetry/README.md#getprojectmetricssummary) - getProjectMetricsSummary telemetry
* [getProjectOverview](docs/sdks/telemetry/README.md#getprojectoverview) - getProjectOverview telemetry
* [getUserMetricsSummary](docs/sdks/telemetry/README.md#getusermetricssummary) - getUserMetricsSummary telemetry
* [listAttributeKeys](docs/sdks/telemetry/README.md#listattributekeys) - listAttributeKeys telemetry
* [listFilterOptions](docs/sdks/telemetry/README.md#listfilteroptions) - listFilterOptions telemetry
* [listHooksTraces](docs/sdks/telemetry/README.md#listhookstraces) - listHooksTraces telemetry
* [searchChats](docs/sdks/telemetry/README.md#searchchats) - searchChats telemetry
* [searchLogs](docs/sdks/telemetry/README.md#searchlogs) - searchLogs telemetry
* [searchToolCalls](docs/sdks/telemetry/README.md#searchtoolcalls) - searchToolCalls telemetry
* [searchUsers](docs/sdks/telemetry/README.md#searchusers) - searchUsers telemetry

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
* [listForOrg](docs/sdks/toolsets/README.md#listfororg) - listToolsetsForOrg toolsets
* [removeOAuthServer](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
* [updateBySlug](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
* [updateOAuthProxyServer](docs/sdks/toolsets/README.md#updateoauthproxyserver) - updateOAuthProxyServer toolsets

### [Triggers](docs/sdks/triggers/README.md)

* [create](docs/sdks/triggers/README.md#create) - createTriggerInstance triggers
* [listDefinitions](docs/sdks/triggers/README.md#listdefinitions) - listTriggerDefinitions triggers
* [delete](docs/sdks/triggers/README.md#delete) - deleteTriggerInstance triggers
* [get](docs/sdks/triggers/README.md#get) - getTriggerInstance triggers
* [list](docs/sdks/triggers/README.md#list) - listTriggerInstances triggers
* [pause](docs/sdks/triggers/README.md#pause) - pauseTriggerInstance triggers
* [resume](docs/sdks/triggers/README.md#resume) - resumeTriggerInstance triggers
* [update](docs/sdks/triggers/README.md#update) - updateTriggerInstance triggers

### [Usage](docs/sdks/usage/README.md)

* [createCheckout](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
* [createCustomerSession](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
* [createTopUpCheckout](docs/sdks/usage/README.md#createtopupcheckout) - createTopUpCheckout usage
* [getPeriodUsage](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
* [getUsageTiers](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage

### [UserSessionClients](docs/sdks/usersessionclients/README.md)

* [get](docs/sdks/usersessionclients/README.md#get) - getUserSessionClient userSessionClients
* [list](docs/sdks/usersessionclients/README.md#list) - listUserSessionClients userSessionClients
* [revoke](docs/sdks/usersessionclients/README.md#revoke) - revokeUserSessionClient userSessionClients

### [UserSessionConsents](docs/sdks/usersessionconsents/README.md)

* [list](docs/sdks/usersessionconsents/README.md#list) - listUserSessionConsents userSessionConsents
* [revoke](docs/sdks/usersessionconsents/README.md#revoke) - revokeUserSessionConsent userSessionConsents

### [UserSessionIssuers](docs/sdks/usersessionissuers/README.md)

* [create](docs/sdks/usersessionissuers/README.md#create) - createUserSessionIssuer userSessionIssuers
* [delete](docs/sdks/usersessionissuers/README.md#delete) - deleteUserSessionIssuer userSessionIssuers
* [get](docs/sdks/usersessionissuers/README.md#get) - getUserSessionIssuer userSessionIssuers
* [list](docs/sdks/usersessionissuers/README.md#list) - listUserSessionIssuers userSessionIssuers
* [update](docs/sdks/usersessionissuers/README.md#update) - updateUserSessionIssuer userSessionIssuers

### [UserSessions](docs/sdks/usersessions/README.md)

* [list](docs/sdks/usersessions/README.md#list) - listUserSessions userSessions
* [revoke](docs/sdks/usersessions/README.md#revoke) - revokeUserSession userSessions

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

- [`accessCreateRole`](docs/sdks/access/README.md#createrole) - createRole access
- [`accessDeleteRole`](docs/sdks/access/README.md#deleterole) - deleteRole access
- [`accessDisableRBAC`](docs/sdks/access/README.md#disablerbac) - disableRBAC access
- [`accessEnableRBAC`](docs/sdks/access/README.md#enablerbac) - enableRBAC access
- [`accessGetRBACStatus`](docs/sdks/access/README.md#getrbacstatus) - getRBACStatus access
- [`accessGetRole`](docs/sdks/access/README.md#getrole) - getRole access
- [`accessListChallenges`](docs/sdks/access/README.md#listchallenges) - listChallenges access
- [`accessListGrants`](docs/sdks/access/README.md#listgrants) - listGrants access
- [`accessListMembers`](docs/sdks/access/README.md#listmembers) - listMembers access
- [`accessListRoles`](docs/sdks/access/README.md#listroles) - listRoles access
- [`accessListScopes`](docs/sdks/access/README.md#listscopes) - listScopes access
- [`accessResolveChallenge`](docs/sdks/access/README.md#resolvechallenge) - resolveChallenge access
- [`accessUpdateMemberRole`](docs/sdks/access/README.md#updatememberrole) - updateMemberRole access
- [`accessUpdateRole`](docs/sdks/access/README.md#updaterole) - updateRole access
- [`assetsCreateSignedChatAttachmentURL`](docs/sdks/assets/README.md#createsignedchatattachmenturl) - createSignedChatAttachmentURL assets
- [`assetsFetchOpenAPIv3FromURL`](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
- [`assetsListAssets`](docs/sdks/assets/README.md#listassets) - listAssets assets
- [`assetsServeChatAttachment`](docs/sdks/assets/README.md#servechatattachment) - serveChatAttachment assets
- [`assetsServeChatAttachmentSigned`](docs/sdks/assets/README.md#servechatattachmentsigned) - serveChatAttachmentSigned assets
- [`assetsServeFunction`](docs/sdks/assets/README.md#servefunction) - serveFunction assets
- [`assetsServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`assetsServeOpenAPIv3`](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
- [`assetsUploadChatAttachment`](docs/sdks/assets/README.md#uploadchatattachment) - uploadChatAttachment assets
- [`assetsUploadFunctions`](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
- [`assetsUploadImage`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`assetsUploadOpenAPIv3`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets
- [`assistantsCreate`](docs/sdks/assistants/README.md#create) - createAssistant assistants
- [`assistantsDelete`](docs/sdks/assistants/README.md#delete) - deleteAssistant assistants
- [`assistantsGet`](docs/sdks/assistants/README.md#get) - getAssistant assistants
- [`assistantsList`](docs/sdks/assistants/README.md#list) - listAssistants assistants
- [`assistantsUpdate`](docs/sdks/assistants/README.md#update) - updateAssistant assistants
- [`auditlogsList`](docs/sdks/auditlogs/README.md#list) - list auditlogs
- [`auditlogsListFacets`](docs/sdks/auditlogs/README.md#listfacets) - listFacets auditlogs
- [`authCallback`](docs/sdks/auth/README.md#callback) - callback auth
- [`authInfo`](docs/sdks/auth/README.md#info) - info auth
- [`authLogin`](docs/sdks/auth/README.md#login) - login auth
- [`authLogout`](docs/sdks/auth/README.md#logout) - logout auth
- [`authRegister`](docs/sdks/auth/README.md#register) - register auth
- [`authSwitchScopes`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`chatCreditUsage`](docs/sdks/chat/README.md#creditusage) - creditUsage chat
- [`chatDelete`](docs/sdks/chat/README.md#delete) - deleteChat chat
- [`chatGenerateTitle`](docs/sdks/chat/README.md#generatetitle) - generateTitle chat
- [`chatList`](docs/sdks/chat/README.md#list) - listChats chat
- [`chatListChatsWithResolutions`](docs/sdks/chat/README.md#listchatswithresolutions) - listChatsWithResolutions chat
- [`chatLoad`](docs/sdks/chat/README.md#load) - loadChat chat
- [`chatSessionsCreate`](docs/sdks/chatsessions/README.md#create) - create chatSessions
- [`chatSessionsRevoke`](docs/sdks/chatsessions/README.md#revoke) - revoke chatSessions
- [`chatSubmitFeedback`](docs/sdks/chat/README.md#submitfeedback) - submitFeedback chat
- [`collectionsAttachServer`](docs/sdks/collections/README.md#attachserver) - attachServer collections
- [`collectionsCreate`](docs/sdks/collections/README.md#create) - create collections
- [`collectionsDelete`](docs/sdks/collections/README.md#delete) - delete collections
- [`collectionsDetachServer`](docs/sdks/collections/README.md#detachserver) - detachServer collections
- [`collectionsList`](docs/sdks/collections/README.md#list) - list collections
- [`collectionsListServers`](docs/sdks/collections/README.md#listservers) - listServers collections
- [`collectionsUpdate`](docs/sdks/collections/README.md#update) - update collections
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
- [`featuresGet`](docs/sdks/features/README.md#get) - getProductFeatures features
- [`featuresSet`](docs/sdks/features/README.md#set) - setProductFeature features
- [`hooksHooksNumberClaude`](docs/sdks/hooks/README.md#hooksnumberclaude) - claude hooks
- [`hooksHooksNumberCursor`](docs/sdks/hooks/README.md#hooksnumbercursor) - cursor hooks
- [`hooksHooksNumberLogs`](docs/sdks/hooks/README.md#hooksnumberlogs) - logs hooks
- [`hooksHooksNumberMetrics`](docs/sdks/hooks/README.md#hooksnumbermetrics) - metrics hooks
- [`hooksServerNamesDeleteServerNameOverride`](docs/sdks/hooksservernames/README.md#deleteservernameoverride) - delete hooksServerNames
- [`hooksServerNamesListServerNameOverrides`](docs/sdks/hooksservernames/README.md#listservernameoverrides) - list hooksServerNames
- [`hooksServerNamesUpsertServerNameOverride`](docs/sdks/hooksservernames/README.md#upsertservernameoverride) - upsert hooksServerNames
- [`instancesGetBySlug`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`integrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`integrationsList`](docs/sdks/integrations/README.md#list) - list integrations
- [`keysCreate`](docs/sdks/keys/README.md#create) - createKey keys
- [`keysList`](docs/sdks/keys/README.md#list) - listKeys keys
- [`keysRevokeById`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`keysValidate`](docs/sdks/keys/README.md#validate) - verifyKey keys
- [`mcpEndpointsCreate`](docs/sdks/mcpendpoints/README.md#create) - createMcpEndpoint mcpEndpoints
- [`mcpEndpointsDelete`](docs/sdks/mcpendpoints/README.md#delete) - deleteMcpEndpoint mcpEndpoints
- [`mcpEndpointsGet`](docs/sdks/mcpendpoints/README.md#get) - getMcpEndpoint mcpEndpoints
- [`mcpEndpointsList`](docs/sdks/mcpendpoints/README.md#list) - listMcpEndpoints mcpEndpoints
- [`mcpEndpointsUpdate`](docs/sdks/mcpendpoints/README.md#update) - updateMcpEndpoint mcpEndpoints
- [`mcpMetadataExport`](docs/sdks/mcpmetadata/README.md#export) - exportMcpMetadata mcpMetadata
- [`mcpMetadataGet`](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
- [`mcpMetadataSet`](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata
- [`mcpRegistriesClearCache`](docs/sdks/mcpregistries/README.md#clearcache) - clearCache mcpRegistries
- [`mcpRegistriesGetServerDetails`](docs/sdks/mcpregistries/README.md#getserverdetails) - getServerDetails mcpRegistries
- [`mcpRegistriesListCatalog`](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries
- [`mcpRegistriesListRegistries`](docs/sdks/mcpregistries/README.md#listregistries) - listRegistries mcpRegistries
- [`mcpServersCreate`](docs/sdks/mcpservers/README.md#create) - createMcpServer mcpServers
- [`mcpServersDelete`](docs/sdks/mcpservers/README.md#delete) - deleteMcpServer mcpServers
- [`mcpServersGet`](docs/sdks/mcpservers/README.md#get) - getMcpServer mcpServers
- [`mcpServersList`](docs/sdks/mcpservers/README.md#list) - listMcpServers mcpServers
- [`mcpServersUpdate`](docs/sdks/mcpservers/README.md#update) - updateMcpServer mcpServers
- [`organizationsGetInviteByToken`](docs/sdks/organizations/README.md#getinvitebytoken) - getInviteByToken organizations
- [`organizationsListInvites`](docs/sdks/organizations/README.md#listinvites) - listInvites organizations
- [`organizationsListUsers`](docs/sdks/organizations/README.md#listusers) - listUsers organizations
- [`organizationsRemoveUser`](docs/sdks/organizations/README.md#removeuser) - removeUser organizations
- [`organizationsRevokeInvite`](docs/sdks/organizations/README.md#revokeinvite) - revokeInvite organizations
- [`organizationsSendInvite`](docs/sdks/organizations/README.md#sendinvite) - sendInvite organizations
- [`packagesCreate`](docs/sdks/packages/README.md#create) - createPackage packages
- [`packagesList`](docs/sdks/packages/README.md#list) - listPackages packages
- [`packagesListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`packagesPublish`](docs/sdks/packages/README.md#publish) - publish packages
- [`packagesUpdate`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`pluginsAddPluginServer`](docs/sdks/plugins/README.md#addpluginserver) - addPluginServer plugins
- [`pluginsCreatePlugin`](docs/sdks/plugins/README.md#createplugin) - createPlugin plugins
- [`pluginsDeletePlugin`](docs/sdks/plugins/README.md#deleteplugin) - deletePlugin plugins
- [`pluginsDownloadObservabilityPlugin`](docs/sdks/plugins/README.md#downloadobservabilityplugin) - downloadObservabilityPlugin plugins
- [`pluginsDownloadPluginPackage`](docs/sdks/plugins/README.md#downloadpluginpackage) - downloadPluginPackage plugins
- [`pluginsGetPlugin`](docs/sdks/plugins/README.md#getplugin) - getPlugin plugins
- [`pluginsGetPublishStatus`](docs/sdks/plugins/README.md#getpublishstatus) - getPublishStatus plugins
- [`pluginsListPlugins`](docs/sdks/plugins/README.md#listplugins) - listPlugins plugins
- [`pluginsPublishPlugins`](docs/sdks/plugins/README.md#publishplugins) - publishPlugins plugins
- [`pluginsRemovePluginServer`](docs/sdks/plugins/README.md#removepluginserver) - removePluginServer plugins
- [`pluginsSetPluginAssignments`](docs/sdks/plugins/README.md#setpluginassignments) - setPluginAssignments plugins
- [`pluginsUpdatePlugin`](docs/sdks/plugins/README.md#updateplugin) - updatePlugin plugins
- [`pluginsUpdatePluginServer`](docs/sdks/plugins/README.md#updatepluginserver) - updatePluginServer plugins
- [`projectsCreate`](docs/sdks/projects/README.md#create) - createProject projects
- [`projectsDeleteById`](docs/sdks/projects/README.md#deletebyid) - deleteProject projects
- [`projectsList`](docs/sdks/projects/README.md#list) - listProjects projects
- [`projectsListAllowedOrigins`](docs/sdks/projects/README.md#listallowedorigins) - listAllowedOrigins projects
- [`projectsRead`](docs/sdks/projects/README.md#read) - getProject projects
- [`projectsSetLogo`](docs/sdks/projects/README.md#setlogo) - setLogo projects
- [`projectsSetOrganizationWhitelist`](docs/sdks/projects/README.md#setorganizationwhitelist) - setOrganizationWhitelist projects
- [`projectsUpsertAllowedOrigin`](docs/sdks/projects/README.md#upsertallowedorigin) - upsertAllowedOrigin projects
- [`remoteMcpCreateServer`](docs/sdks/remotemcp/README.md#createserver) - createServer remoteMcp
- [`remoteMcpDeleteServer`](docs/sdks/remotemcp/README.md#deleteserver) - deleteServer remoteMcp
- [`remoteMcpGetServer`](docs/sdks/remotemcp/README.md#getserver) - getServer remoteMcp
- [`remoteMcpListServers`](docs/sdks/remotemcp/README.md#listservers) - listServers remoteMcp
- [`remoteMcpUpdateServer`](docs/sdks/remotemcp/README.md#updateserver) - updateServer remoteMcp
- [`resourcesList`](docs/sdks/resources/README.md#list) - listResources resources
- [`riskPoliciesCreate`](docs/sdks/policies/README.md#create) - createRiskPolicy risk
- [`riskPoliciesDelete`](docs/sdks/policies/README.md#delete) - deleteRiskPolicy risk
- [`riskPoliciesGet`](docs/sdks/policies/README.md#get) - getRiskPolicy risk
- [`riskPoliciesList`](docs/sdks/policies/README.md#list) - listRiskPolicies risk
- [`riskPoliciesStatus`](docs/sdks/policies/README.md#status) - getRiskPolicyStatus risk
- [`riskPoliciesTrigger`](docs/sdks/policies/README.md#trigger) - triggerRiskAnalysis risk
- [`riskPoliciesUpdate`](docs/sdks/policies/README.md#update) - updateRiskPolicy risk
- [`riskResultsByChat`](docs/sdks/results/README.md#bychat) - listRiskResultsByChat risk
- [`riskResultsList`](docs/sdks/results/README.md#list) - listRiskResults risk
- [`slackConfigureSlackApp`](docs/sdks/slack/README.md#configureslackapp) - configureSlackApp slack
- [`slackCreateSlackApp`](docs/sdks/slack/README.md#createslackapp) - createSlackApp slack
- [`slackDeleteSlackApp`](docs/sdks/slack/README.md#deleteslackapp) - deleteSlackApp slack
- [`slackGetSlackApp`](docs/sdks/slack/README.md#getslackapp) - getSlackApp slack
- [`slackListSlackApps`](docs/sdks/slack/README.md#listslackapps) - listSlackApps slack
- [`slackUpdateSlackApp`](docs/sdks/slack/README.md#updateslackapp) - updateSlackApp slack
- [`telemetryCaptureEvent`](docs/sdks/telemetry/README.md#captureevent) - captureEvent telemetry
- [`telemetryGetHooksSummary`](docs/sdks/telemetry/README.md#gethookssummary) - getHooksSummary telemetry
- [`telemetryGetObservabilityOverview`](docs/sdks/telemetry/README.md#getobservabilityoverview) - getObservabilityOverview telemetry
- [`telemetryGetProjectMetricsSummary`](docs/sdks/telemetry/README.md#getprojectmetricssummary) - getProjectMetricsSummary telemetry
- [`telemetryGetProjectOverview`](docs/sdks/telemetry/README.md#getprojectoverview) - getProjectOverview telemetry
- [`telemetryGetUserMetricsSummary`](docs/sdks/telemetry/README.md#getusermetricssummary) - getUserMetricsSummary telemetry
- [`telemetryListAttributeKeys`](docs/sdks/telemetry/README.md#listattributekeys) - listAttributeKeys telemetry
- [`telemetryListFilterOptions`](docs/sdks/telemetry/README.md#listfilteroptions) - listFilterOptions telemetry
- [`telemetryListHooksTraces`](docs/sdks/telemetry/README.md#listhookstraces) - listHooksTraces telemetry
- [`telemetrySearchChats`](docs/sdks/telemetry/README.md#searchchats) - searchChats telemetry
- [`telemetrySearchLogs`](docs/sdks/telemetry/README.md#searchlogs) - searchLogs telemetry
- [`telemetrySearchToolCalls`](docs/sdks/telemetry/README.md#searchtoolcalls) - searchToolCalls telemetry
- [`telemetrySearchUsers`](docs/sdks/telemetry/README.md#searchusers) - searchUsers telemetry
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
- [`toolsetsListForOrg`](docs/sdks/toolsets/README.md#listfororg) - listToolsetsForOrg toolsets
- [`toolsetsRemoveOAuthServer`](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
- [`toolsetsUpdateBySlug`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`toolsetsUpdateOAuthProxyServer`](docs/sdks/toolsets/README.md#updateoauthproxyserver) - updateOAuthProxyServer toolsets
- [`toolsList`](docs/sdks/tools/README.md#list) - listTools tools
- [`triggersCreate`](docs/sdks/triggers/README.md#create) - createTriggerInstance triggers
- [`triggersDelete`](docs/sdks/triggers/README.md#delete) - deleteTriggerInstance triggers
- [`triggersGet`](docs/sdks/triggers/README.md#get) - getTriggerInstance triggers
- [`triggersList`](docs/sdks/triggers/README.md#list) - listTriggerInstances triggers
- [`triggersListDefinitions`](docs/sdks/triggers/README.md#listdefinitions) - listTriggerDefinitions triggers
- [`triggersPause`](docs/sdks/triggers/README.md#pause) - pauseTriggerInstance triggers
- [`triggersResume`](docs/sdks/triggers/README.md#resume) - resumeTriggerInstance triggers
- [`triggersUpdate`](docs/sdks/triggers/README.md#update) - updateTriggerInstance triggers
- [`usageCreateCheckout`](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
- [`usageCreateCustomerSession`](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
- [`usageCreateTopUpCheckout`](docs/sdks/usage/README.md#createtopupcheckout) - createTopUpCheckout usage
- [`usageGetPeriodUsage`](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
- [`usageGetUsageTiers`](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage
- [`userSessionClientsGet`](docs/sdks/usersessionclients/README.md#get) - getUserSessionClient userSessionClients
- [`userSessionClientsList`](docs/sdks/usersessionclients/README.md#list) - listUserSessionClients userSessionClients
- [`userSessionClientsRevoke`](docs/sdks/usersessionclients/README.md#revoke) - revokeUserSessionClient userSessionClients
- [`userSessionConsentsList`](docs/sdks/usersessionconsents/README.md#list) - listUserSessionConsents userSessionConsents
- [`userSessionConsentsRevoke`](docs/sdks/usersessionconsents/README.md#revoke) - revokeUserSessionConsent userSessionConsents
- [`userSessionIssuersCreate`](docs/sdks/usersessionissuers/README.md#create) - createUserSessionIssuer userSessionIssuers
- [`userSessionIssuersDelete`](docs/sdks/usersessionissuers/README.md#delete) - deleteUserSessionIssuer userSessionIssuers
- [`userSessionIssuersGet`](docs/sdks/usersessionissuers/README.md#get) - getUserSessionIssuer userSessionIssuers
- [`userSessionIssuersList`](docs/sdks/usersessionissuers/README.md#list) - listUserSessionIssuers userSessionIssuers
- [`userSessionIssuersUpdate`](docs/sdks/usersessionissuers/README.md#update) - updateUserSessionIssuer userSessionIssuers
- [`userSessionsList`](docs/sdks/usersessions/README.md#list) - listUserSessions userSessions
- [`userSessionsRevoke`](docs/sdks/usersessions/README.md#revoke) - revokeUserSession userSessions
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
- [`useAddPluginServerMutation`](docs/sdks/plugins/README.md#addpluginserver) - addPluginServer plugins
- [`useAssistantsCreateMutation`](docs/sdks/assistants/README.md#create) - createAssistant assistants
- [`useAssistantsDeleteMutation`](docs/sdks/assistants/README.md#delete) - deleteAssistant assistants
- [`useAssistantsGet`](docs/sdks/assistants/README.md#get) - getAssistant assistants
- [`useAssistantsList`](docs/sdks/assistants/README.md#list) - listAssistants assistants
- [`useAssistantsUpdateMutation`](docs/sdks/assistants/README.md#update) - updateAssistant assistants
- [`useAuditLogFacets`](docs/sdks/auditlogs/README.md#listfacets) - listFacets auditlogs
- [`useAuditLogs`](docs/sdks/auditlogs/README.md#list) - list auditlogs
- [`useChallenges`](docs/sdks/access/README.md#listchallenges) - listChallenges access
- [`useChatDeleteMutation`](docs/sdks/chat/README.md#delete) - deleteChat chat
- [`useChatGenerateTitleMutation`](docs/sdks/chat/README.md#generatetitle) - generateTitle chat
- [`useChatSessionsCreateMutation`](docs/sdks/chatsessions/README.md#create) - create chatSessions
- [`useChatSessionsRevokeMutation`](docs/sdks/chatsessions/README.md#revoke) - revoke chatSessions
- [`useChatSubmitFeedbackMutation`](docs/sdks/chat/README.md#submitfeedback) - submitFeedback chat
- [`useCheckMCPSlugAvailability`](docs/sdks/toolsets/README.md#checkmcpslugavailability) - checkMCPSlugAvailability toolsets
- [`useCloneToolsetMutation`](docs/sdks/toolsets/README.md#clonebyslug) - cloneToolset toolsets
- [`useCollectionsAttachServerMutation`](docs/sdks/collections/README.md#attachserver) - attachServer collections
- [`useCollectionsCreateMutation`](docs/sdks/collections/README.md#create) - create collections
- [`useCollectionsDeleteMutation`](docs/sdks/collections/README.md#delete) - delete collections
- [`useCollectionsDetachServerMutation`](docs/sdks/collections/README.md#detachserver) - detachServer collections
- [`useCollectionsListServers`](docs/sdks/collections/README.md#listservers) - listServers collections
- [`useCollectionsUpdateMutation`](docs/sdks/collections/README.md#update) - update collections
- [`useConfigureSlackAppMutation`](docs/sdks/slack/README.md#configureslackapp) - configureSlackApp slack
- [`useCreateAPIKeyMutation`](docs/sdks/keys/README.md#create) - createKey keys
- [`useCreateCheckoutMutation`](docs/sdks/usage/README.md#createcheckout) - createCheckout usage
- [`useCreateCustomerSessionMutation`](docs/sdks/usage/README.md#createcustomersession) - createCustomerSession usage
- [`useCreateDeploymentMutation`](docs/sdks/deployments/README.md#create) - createDeployment deployments
- [`useCreateEnvironmentMutation`](docs/sdks/environments/README.md#create) - createEnvironment environments
- [`useCreateMcpEndpointMutation`](docs/sdks/mcpendpoints/README.md#create) - createMcpEndpoint mcpEndpoints
- [`useCreateMcpServerMutation`](docs/sdks/mcpservers/README.md#create) - createMcpServer mcpServers
- [`useCreatePackageMutation`](docs/sdks/packages/README.md#create) - createPackage packages
- [`useCreatePluginMutation`](docs/sdks/plugins/README.md#createplugin) - createPlugin plugins
- [`useCreateProjectMutation`](docs/sdks/projects/README.md#create) - createProject projects
- [`useCreateRemoteMcpServerMutation`](docs/sdks/remotemcp/README.md#createserver) - createServer remoteMcp
- [`useCreateRoleMutation`](docs/sdks/access/README.md#createrole) - createRole access
- [`useCreateSignedChatAttachmentURLMutation`](docs/sdks/assets/README.md#createsignedchatattachmenturl) - createSignedChatAttachmentURL assets
- [`useCreateSlackAppMutation`](docs/sdks/slack/README.md#createslackapp) - createSlackApp slack
- [`useCreateTemplateMutation`](docs/sdks/templates/README.md#create) - createTemplate templates
- [`useCreateToolsetMutation`](docs/sdks/toolsets/README.md#create) - createToolset toolsets
- [`useCreateTopUpCheckoutMutation`](docs/sdks/usage/README.md#createtopupcheckout) - createTopUpCheckout usage
- [`useCreateTriggerMutation`](docs/sdks/triggers/README.md#create) - createTriggerInstance triggers
- [`useCreateUserSessionIssuerMutation`](docs/sdks/usersessionissuers/README.md#create) - createUserSessionIssuer userSessionIssuers
- [`useDeleteDomainMutation`](docs/sdks/domains/README.md#deletedomain) - deleteDomain domains
- [`useDeleteEnvironmentMutation`](docs/sdks/environments/README.md#deletebyslug) - deleteEnvironment environments
- [`useDeleteGlobalVariationMutation`](docs/sdks/variations/README.md#deleteglobal) - deleteGlobal variations
- [`useDeleteMcpEndpointMutation`](docs/sdks/mcpendpoints/README.md#delete) - deleteMcpEndpoint mcpEndpoints
- [`useDeleteMcpServerMutation`](docs/sdks/mcpservers/README.md#delete) - deleteMcpServer mcpServers
- [`useDeletePluginMutation`](docs/sdks/plugins/README.md#deleteplugin) - deletePlugin plugins
- [`useDeleteProjectMutation`](docs/sdks/projects/README.md#deletebyid) - deleteProject projects
- [`useDeleteRemoteMcpServerMutation`](docs/sdks/remotemcp/README.md#deleteserver) - deleteServer remoteMcp
- [`useDeleteRoleMutation`](docs/sdks/access/README.md#deleterole) - deleteRole access
- [`useDeleteSlackAppMutation`](docs/sdks/slack/README.md#deleteslackapp) - deleteSlackApp slack
- [`useDeleteSourceEnvironmentLinkMutation`](docs/sdks/environments/README.md#deletesourcelink) - deleteSourceEnvironmentLink environments
- [`useDeleteTemplateMutation`](docs/sdks/templates/README.md#delete) - deleteTemplate templates
- [`useDeleteToolsetEnvironmentLinkMutation`](docs/sdks/environments/README.md#deletetoolsetlink) - deleteToolsetEnvironmentLink environments
- [`useDeleteToolsetMutation`](docs/sdks/toolsets/README.md#deletebyslug) - deleteToolset toolsets
- [`useDeleteTriggerMutation`](docs/sdks/triggers/README.md#delete) - deleteTriggerInstance triggers
- [`useDeleteUserSessionIssuerMutation`](docs/sdks/usersessionissuers/README.md#delete) - deleteUserSessionIssuer userSessionIssuers
- [`useDeployment`](docs/sdks/deployments/README.md#getbyid) - getDeployment deployments
- [`useDeploymentLogs`](docs/sdks/deployments/README.md#logs) - getDeploymentLogs deployments
- [`useDisableRBACMutation`](docs/sdks/access/README.md#disablerbac) - disableRBAC access
- [`useEnableRBACMutation`](docs/sdks/access/README.md#enablerbac) - enableRBAC access
- [`useEvolveDeploymentMutation`](docs/sdks/deployments/README.md#evolvedeployment) - evolve deployments
- [`useExportMcpMetadataMutation`](docs/sdks/mcpmetadata/README.md#export) - exportMcpMetadata mcpMetadata
- [`useFeaturesGet`](docs/sdks/features/README.md#get) - getProductFeatures features
- [`useFeaturesSetMutation`](docs/sdks/features/README.md#set) - setProductFeature features
- [`useFetchOpenAPIv3FromURLMutation`](docs/sdks/assets/README.md#fetchopenapiv3fromurl) - fetchOpenAPIv3FromURL assets
- [`useGetCreditUsage`](docs/sdks/chat/README.md#creditusage) - creditUsage chat
- [`useGetDomain`](docs/sdks/domains/README.md#getdomain) - getDomain domains
- [`useGetHooksSummary`](docs/sdks/telemetry/README.md#gethookssummary) - getHooksSummary telemetry
- [`useGetInviteByToken`](docs/sdks/organizations/README.md#getinvitebytoken) - getInviteByToken organizations
- [`useGetMcpEndpoint`](docs/sdks/mcpendpoints/README.md#get) - getMcpEndpoint mcpEndpoints
- [`useGetMcpMetadata`](docs/sdks/mcpmetadata/README.md#get) - getMcpMetadata mcpMetadata
- [`useGetMcpServer`](docs/sdks/mcpservers/README.md#get) - getMcpServer mcpServers
- [`useGetObservabilityOverview`](docs/sdks/telemetry/README.md#getobservabilityoverview) - getObservabilityOverview telemetry
- [`useGetPeriodUsage`](docs/sdks/usage/README.md#getperiodusage) - getPeriodUsage usage
- [`useGetProjectMetricsSummary`](docs/sdks/telemetry/README.md#getprojectmetricssummary) - getProjectMetricsSummary telemetry
- [`useGetProjectOverview`](docs/sdks/telemetry/README.md#getprojectoverview) - getProjectOverview telemetry
- [`useGetRemoteMcpServer`](docs/sdks/remotemcp/README.md#getserver) - getServer remoteMcp
- [`useGetSlackApp`](docs/sdks/slack/README.md#getslackapp) - getSlackApp slack
- [`useGetSourceEnvironment`](docs/sdks/environments/README.md#getbysource) - getSourceEnvironment environments
- [`useGetToolsetEnvironment`](docs/sdks/environments/README.md#getbytoolset) - getToolsetEnvironment environments
- [`useGetUsageTiers`](docs/sdks/usage/README.md#getusagetiers) - getUsageTiers usage
- [`useGetUserMetricsSummary`](docs/sdks/telemetry/README.md#getusermetricssummary) - getUserMetricsSummary telemetry
- [`useGlobalVariations`](docs/sdks/variations/README.md#listglobal) - listGlobal variations
- [`useGrants`](docs/sdks/access/README.md#listgrants) - listGrants access
- [`useHooksHooksNumberClaudeMutation`](docs/sdks/hooks/README.md#hooksnumberclaude) - claude hooks
- [`useHooksHooksNumberCursorMutation`](docs/sdks/hooks/README.md#hooksnumbercursor) - cursor hooks
- [`useHooksHooksNumberLogsMutation`](docs/sdks/hooks/README.md#hooksnumberlogs) - logs hooks
- [`useHooksHooksNumberMetricsMutation`](docs/sdks/hooks/README.md#hooksnumbermetrics) - metrics hooks
- [`useHooksServerNamesDeleteServerNameOverrideMutation`](docs/sdks/hooksservernames/README.md#deleteservernameoverride) - delete hooksServerNames
- [`useHooksServerNamesListServerNameOverrides`](docs/sdks/hooksservernames/README.md#listservernameoverrides) - list hooksServerNames
- [`useHooksServerNamesUpsertServerNameOverrideMutation`](docs/sdks/hooksservernames/README.md#upsertservernameoverride) - upsert hooksServerNames
- [`useInstance`](docs/sdks/instances/README.md#getbyslug) - getInstance instances
- [`useIntegrationsIntegrationsNumberGet`](docs/sdks/integrations/README.md#integrationsnumberget) - get integrations
- [`useLatestDeployment`](docs/sdks/deployments/README.md#latest) - getLatestDeployment deployments
- [`useListAllowedOrigins`](docs/sdks/projects/README.md#listallowedorigins) - listAllowedOrigins projects
- [`useListAPIKeys`](docs/sdks/keys/README.md#list) - listKeys keys
- [`useListAssets`](docs/sdks/assets/README.md#listassets) - listAssets assets
- [`useListAttributeKeys`](docs/sdks/telemetry/README.md#listattributekeys) - listAttributeKeys telemetry
- [`useListChats`](docs/sdks/chat/README.md#list) - listChats chat
- [`useListChatsWithResolutions`](docs/sdks/chat/README.md#listchatswithresolutions) - listChatsWithResolutions chat
- [`useListCollections`](docs/sdks/collections/README.md#list) - list collections
- [`useListDeployments`](docs/sdks/deployments/README.md#list) - listDeployments deployments
- [`useListEnvironments`](docs/sdks/environments/README.md#list) - listEnvironments environments
- [`useListFilterOptions`](docs/sdks/telemetry/README.md#listfilteroptions) - listFilterOptions telemetry
- [`useListHooksTraces`](docs/sdks/telemetry/README.md#listhookstraces) - listHooksTraces telemetry
- [`useListIntegrations`](docs/sdks/integrations/README.md#list) - list integrations
- [`useListInvites`](docs/sdks/organizations/README.md#listinvites) - listInvites organizations
- [`useListMCPCatalog`](docs/sdks/mcpregistries/README.md#listcatalog) - listCatalog mcpRegistries
- [`useListMCPRegistries`](docs/sdks/mcpregistries/README.md#listregistries) - listRegistries mcpRegistries
- [`useListOrganizationUsers`](docs/sdks/organizations/README.md#listusers) - listUsers organizations
- [`useListPackages`](docs/sdks/packages/README.md#list) - listPackages packages
- [`useListProjects`](docs/sdks/projects/README.md#list) - listProjects projects
- [`useListResources`](docs/sdks/resources/README.md#list) - listResources resources
- [`useListScopes`](docs/sdks/access/README.md#listscopes) - listScopes access
- [`useListSlackApps`](docs/sdks/slack/README.md#listslackapps) - listSlackApps slack
- [`useListTools`](docs/sdks/tools/README.md#list) - listTools tools
- [`useListToolsets`](docs/sdks/toolsets/README.md#list) - listToolsets toolsets
- [`useListToolsetsForOrg`](docs/sdks/toolsets/README.md#listfororg) - listToolsetsForOrg toolsets
- [`useListVersions`](docs/sdks/packages/README.md#listversions) - listVersions packages
- [`useLoadChat`](docs/sdks/chat/README.md#load) - loadChat chat
- [`useLogoutMutation`](docs/sdks/auth/README.md#logout) - logout auth
- [`useMcpEndpoints`](docs/sdks/mcpendpoints/README.md#list) - listMcpEndpoints mcpEndpoints
- [`useMcpMetadataSetMutation`](docs/sdks/mcpmetadata/README.md#set) - setMcpMetadata mcpMetadata
- [`useMcpRegistriesClearCacheMutation`](docs/sdks/mcpregistries/README.md#clearcache) - clearCache mcpRegistries
- [`useMcpRegistriesGetServerDetails`](docs/sdks/mcpregistries/README.md#getserverdetails) - getServerDetails mcpRegistries
- [`useMcpServers`](docs/sdks/mcpservers/README.md#list) - listMcpServers mcpServers
- [`useMembers`](docs/sdks/access/README.md#listmembers) - listMembers access
- [`usePauseTriggerMutation`](docs/sdks/triggers/README.md#pause) - pauseTriggerInstance triggers
- [`usePlugin`](docs/sdks/plugins/README.md#getplugin) - getPlugin plugins
- [`usePlugins`](docs/sdks/plugins/README.md#listplugins) - listPlugins plugins
- [`usePluginsDownloadObservabilityPlugin`](docs/sdks/plugins/README.md#downloadobservabilityplugin) - downloadObservabilityPlugin plugins
- [`usePluginsDownloadPluginPackage`](docs/sdks/plugins/README.md#downloadpluginpackage) - downloadPluginPackage plugins
- [`useProject`](docs/sdks/projects/README.md#read) - getProject projects
- [`useProjectsSetOrganizationWhitelistMutation`](docs/sdks/projects/README.md#setorganizationwhitelist) - setOrganizationWhitelist projects
- [`usePublishPackageMutation`](docs/sdks/packages/README.md#publish) - publish packages
- [`usePublishPluginsMutation`](docs/sdks/plugins/README.md#publishplugins) - publishPlugins plugins
- [`usePublishStatus`](docs/sdks/plugins/README.md#getpublishstatus) - getPublishStatus plugins
- [`useRbacStatus`](docs/sdks/access/README.md#getrbacstatus) - getRBACStatus access
- [`useRedeployDeploymentMutation`](docs/sdks/deployments/README.md#redeploydeployment) - redeploy deployments
- [`useRegisterDomainMutation`](docs/sdks/domains/README.md#registerdomain) - createDomain domains
- [`useRegisterMutation`](docs/sdks/auth/README.md#register) - register auth
- [`useRemoteMcpServers`](docs/sdks/remotemcp/README.md#listservers) - listServers remoteMcp
- [`useRemoveOAuthServerMutation`](docs/sdks/toolsets/README.md#removeoauthserver) - removeOAuthServer toolsets
- [`useRemoveOrganizationUserMutation`](docs/sdks/organizations/README.md#removeuser) - removeUser organizations
- [`useRemovePluginServerMutation`](docs/sdks/plugins/README.md#removepluginserver) - removePluginServer plugins
- [`useRenderTemplate`](docs/sdks/templates/README.md#render) - renderTemplate templates
- [`useRenderTemplateByID`](docs/sdks/templates/README.md#renderbyid) - renderTemplateByID templates
- [`useResolveChallengeMutation`](docs/sdks/access/README.md#resolvechallenge) - resolveChallenge access
- [`useResumeTriggerMutation`](docs/sdks/triggers/README.md#resume) - resumeTriggerInstance triggers
- [`useRevokeAPIKeyMutation`](docs/sdks/keys/README.md#revokebyid) - revokeKey keys
- [`useRevokeInviteMutation`](docs/sdks/organizations/README.md#revokeinvite) - revokeInvite organizations
- [`useRevokeUserSessionClientMutation`](docs/sdks/usersessionclients/README.md#revoke) - revokeUserSessionClient userSessionClients
- [`useRevokeUserSessionConsentMutation`](docs/sdks/usersessionconsents/README.md#revoke) - revokeUserSessionConsent userSessionConsents
- [`useRevokeUserSessionMutation`](docs/sdks/usersessions/README.md#revoke) - revokeUserSession userSessions
- [`useRiskCreatePolicyMutation`](docs/sdks/policies/README.md#create) - createRiskPolicy risk
- [`useRiskListPolicies`](docs/sdks/policies/README.md#list) - listRiskPolicies risk
- [`useRiskListResults`](docs/sdks/results/README.md#list) - listRiskResults risk
- [`useRiskListResultsByChat`](docs/sdks/results/README.md#bychat) - listRiskResultsByChat risk
- [`useRiskPoliciesDeleteMutation`](docs/sdks/policies/README.md#delete) - deleteRiskPolicy risk
- [`useRiskPoliciesGet`](docs/sdks/policies/README.md#get) - getRiskPolicy risk
- [`useRiskPoliciesStatus`](docs/sdks/policies/README.md#status) - getRiskPolicyStatus risk
- [`useRiskPoliciesTriggerMutation`](docs/sdks/policies/README.md#trigger) - triggerRiskAnalysis risk
- [`useRiskPoliciesUpdateMutation`](docs/sdks/policies/README.md#update) - updateRiskPolicy risk
- [`useRole`](docs/sdks/access/README.md#getrole) - getRole access
- [`useRoles`](docs/sdks/access/README.md#listroles) - listRoles access
- [`useSearchChats`](docs/sdks/telemetry/README.md#searchchats) - searchChats telemetry
- [`useSearchLogsMutation`](docs/sdks/telemetry/README.md#searchlogs) - searchLogs telemetry
- [`useSearchToolCallsMutation`](docs/sdks/telemetry/README.md#searchtoolcalls) - searchToolCalls telemetry
- [`useSearchUsers`](docs/sdks/telemetry/README.md#searchusers) - searchUsers telemetry
- [`useSendInviteMutation`](docs/sdks/organizations/README.md#sendinvite) - sendInvite organizations
- [`useServeChatAttachment`](docs/sdks/assets/README.md#servechatattachment) - serveChatAttachment assets
- [`useServeChatAttachmentSigned`](docs/sdks/assets/README.md#servechatattachmentsigned) - serveChatAttachmentSigned assets
- [`useServeFunction`](docs/sdks/assets/README.md#servefunction) - serveFunction assets
- [`useServeImage`](docs/sdks/assets/README.md#serveimage) - serveImage assets
- [`useServeOpenAPIv3`](docs/sdks/assets/README.md#serveopenapiv3) - serveOpenAPIv3 assets
- [`useSessionInfo`](docs/sdks/auth/README.md#info) - info auth
- [`useSetPluginAssignmentsMutation`](docs/sdks/plugins/README.md#setpluginassignments) - setPluginAssignments plugins
- [`useSetProjectLogoMutation`](docs/sdks/projects/README.md#setlogo) - setLogo projects
- [`useSetSourceEnvironmentLinkMutation`](docs/sdks/environments/README.md#setsourcelink) - setSourceEnvironmentLink environments
- [`useSetToolsetEnvironmentLinkMutation`](docs/sdks/environments/README.md#settoolsetlink) - setToolsetEnvironmentLink environments
- [`useSwitchScopesMutation`](docs/sdks/auth/README.md#switchscopes) - switchScopes auth
- [`useTelemetryCaptureEventMutation`](docs/sdks/telemetry/README.md#captureevent) - captureEvent telemetry
- [`useTemplate`](docs/sdks/templates/README.md#get) - getTemplate templates
- [`useTemplates`](docs/sdks/templates/README.md#list) - listTemplates templates
- [`useToolset`](docs/sdks/toolsets/README.md#getbyslug) - getToolset toolsets
- [`useTrigger`](docs/sdks/triggers/README.md#get) - getTriggerInstance triggers
- [`useTriggerDefinitions`](docs/sdks/triggers/README.md#listdefinitions) - listTriggerDefinitions triggers
- [`useTriggers`](docs/sdks/triggers/README.md#list) - listTriggerInstances triggers
- [`useUpdateEnvironmentMutation`](docs/sdks/environments/README.md#updatebyslug) - updateEnvironment environments
- [`useUpdateMcpEndpointMutation`](docs/sdks/mcpendpoints/README.md#update) - updateMcpEndpoint mcpEndpoints
- [`useUpdateMcpServerMutation`](docs/sdks/mcpservers/README.md#update) - updateMcpServer mcpServers
- [`useUpdateMemberRoleMutation`](docs/sdks/access/README.md#updatememberrole) - updateMemberRole access
- [`useUpdateOAuthProxyServerMutation`](docs/sdks/toolsets/README.md#updateoauthproxyserver) - updateOAuthProxyServer toolsets
- [`useUpdatePackageMutation`](docs/sdks/packages/README.md#update) - updatePackage packages
- [`useUpdatePluginMutation`](docs/sdks/plugins/README.md#updateplugin) - updatePlugin plugins
- [`useUpdatePluginServerMutation`](docs/sdks/plugins/README.md#updatepluginserver) - updatePluginServer plugins
- [`useUpdateRemoteMcpServerMutation`](docs/sdks/remotemcp/README.md#updateserver) - updateServer remoteMcp
- [`useUpdateRoleMutation`](docs/sdks/access/README.md#updaterole) - updateRole access
- [`useUpdateSlackAppMutation`](docs/sdks/slack/README.md#updateslackapp) - updateSlackApp slack
- [`useUpdateTemplateMutation`](docs/sdks/templates/README.md#update) - updateTemplate templates
- [`useUpdateToolsetMutation`](docs/sdks/toolsets/README.md#updatebyslug) - updateToolset toolsets
- [`useUpdateTriggerMutation`](docs/sdks/triggers/README.md#update) - updateTriggerInstance triggers
- [`useUpdateUserSessionIssuerMutation`](docs/sdks/usersessionissuers/README.md#update) - updateUserSessionIssuer userSessionIssuers
- [`useUploadChatAttachmentMutation`](docs/sdks/assets/README.md#uploadchatattachment) - uploadChatAttachment assets
- [`useUploadFunctionsMutation`](docs/sdks/assets/README.md#uploadfunctions) - uploadFunctions assets
- [`useUploadImageMutation`](docs/sdks/assets/README.md#uploadimage) - uploadImage assets
- [`useUploadOpenAPIv3Mutation`](docs/sdks/assets/README.md#uploadopenapiv3) - uploadOpenAPIv3 assets
- [`useUpsertAllowedOriginMutation`](docs/sdks/projects/README.md#upsertallowedorigin) - upsertAllowedOrigin projects
- [`useUpsertGlobalVariationMutation`](docs/sdks/variations/README.md#upsertglobal) - upsertGlobal variations
- [`useUserSessionClient`](docs/sdks/usersessionclients/README.md#get) - getUserSessionClient userSessionClients
- [`useUserSessionClients`](docs/sdks/usersessionclients/README.md#list) - listUserSessionClients userSessionClients
- [`useUserSessionConsents`](docs/sdks/usersessionconsents/README.md#list) - listUserSessionConsents userSessionConsents
- [`useUserSessionIssuer`](docs/sdks/usersessionissuers/README.md#get) - getUserSessionIssuer userSessionIssuers
- [`useUserSessionIssuers`](docs/sdks/usersessionissuers/README.md#list) - listUserSessionIssuers userSessionIssuers
- [`useUserSessions`](docs/sdks/usersessions/README.md#list) - listUserSessions userSessions
- [`useValidateAPIKey`](docs/sdks/keys/README.md#validate) - verifyKey keys

</details>
<!-- End React hooks with TanStack Query [react-query] -->

<!-- Start Pagination [pagination] -->
## Pagination

Some of the endpoints in this SDK support pagination. To use pagination, you
make your SDK calls as usual, but the returned response object will also be an
async iterable that can be consumed using the [`for await...of`][for-await-of]
syntax.

[for-await-of]: https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Statements/for-await...of

Here's an example of one such pagination call:

```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.auditlogs.list();

  for await (const page of result) {
    console.log(page);
  }
}

run();

```
<!-- End Pagination [pagination] -->

<!-- Start File uploads [file-upload] -->
## File uploads

Certain SDK methods accept files as part of a multi-part request. It is possible and typically recommended to upload files as a stream rather than reading the entire contents into memory. This avoids excessive memory consumption and potentially crashing with out-of-memory errors when working with very large files. The following example demonstrates how to attach a file stream to a request.

> [!TIP]
>
> Depending on your JavaScript runtime, there are convenient utilities that return a handle to a file without reading the entire contents into memory:
>
> - **Node.js v20+:** Since v20, Node.js comes with a native `openAsBlob` function in [`node:fs`](https://nodejs.org/docs/latest-v20.x/api/fs.html#fsopenasblobpath-options).
> - **Bun:** The native [`Bun.file`](https://bun.sh/docs/api/file-io#reading-files-bun-file) function produces a file handle that can be used for streaming file uploads.
> - **Browsers:** All supported browsers return an instance to a [`File`](https://developer.mozilla.org/en-US/docs/Web/API/File) when reading the value from an `<input type="file">` element.
> - **Node.js v18:** A file stream can be created using the `fileFrom` helper from [`fetch-blob/from.js`](https://www.npmjs.com/package/fetch-blob).

```typescript
import { Gram } from "@gram/client";
import { openAsBlob } from "node:fs";

const gram = new Gram();

async function run() {
  const result = await gram.assets.uploadFunctions({
    contentLength: 858625,
    requestBody: await openAsBlob("example.file"),
  });

  console.log(result);
}

run();

```
<!-- End File uploads [file-upload] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries.  If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API.  However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a retryConfig object to the call:
```typescript
import { Gram } from "@gram/client";

const gram = new Gram();

async function run() {
  const result = await gram.access.createRole(
    {
      createRoleForm: {
        description: "swerve hm receptor how",
        grants: [
          {
            scope: "mcp:connect",
          },
        ],
        name: "<value>",
      },
    },
    undefined,
    {
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
    },
  );

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
  const result = await gram.access.createRole({
    createRoleForm: {
      description: "swerve hm receptor how",
      grants: [
        {
          scope: "mcp:connect",
        },
      ],
      name: "<value>",
    },
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
    const result = await gram.access.createRole({
      createRoleForm: {
        description: "swerve hm receptor how",
        grants: [
          {
            scope: "mcp:connect",
          },
        ],
        name: "<value>",
      },
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
  serverURL: "https://app.getgram.ai",
});

async function run() {
  const result = await gram.access.createRole({
    createRoleForm: {
      description: "swerve hm receptor how",
      grants: [
        {
          scope: "mcp:connect",
        },
      ],
      name: "<value>",
    },
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

The following example shows how to:
- route requests through a proxy server using [undici](https://www.npmjs.com/package/undici)'s ProxyAgent
- use the `"beforeRequest"` hook to add a custom header and a timeout to requests
- use the `"requestError"` hook to log errors

```typescript
import { Gram } from "@gram/client";
import { ProxyAgent } from "undici";
import { HTTPClient } from "@gram/client/lib/http";

const dispatcher = new ProxyAgent("http://proxy.example.com:8080");

const httpClient = new HTTPClient({
  // 'fetcher' takes a function that has the same signature as native 'fetch'.
  fetcher: (input, init) =>
    // 'dispatcher' is specific to undici and not part of the standard Fetch API.
    fetch(input, { ...init, dispatcher } as RequestInit),
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
