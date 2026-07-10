# ExternalMCPToolDefinition

A proxy tool that references an external MCP server

## Example Usage

```typescript
import { ExternalMCPToolDefinition } from "@gram/client/models/components/externalmcptooldefinition.js";

let value: ExternalMCPToolDefinition = {
  canonicalName: "<value>",
  createdAt: new Date("2026-07-07T10:46:48.668Z"),
  deploymentExternalMcpId: "<id>",
  deploymentId: "<id>",
  description: "mean atop hmph truly like",
  id: "<id>",
  name: "<value>",
  oauthVersion: "<value>",
  projectId: "<id>",
  registryId: "<id>",
  registryServerName: "<value>",
  registrySpecifier: "<value>",
  remoteUrl: "https://overcooked-tuber.biz/",
  requiresOauth: false,
  schema: "<value>",
  slug: "<value>",
  toolUrn: "<value>",
  transportType: "streamable-http",
  updatedAt: new Date("2024-03-12T13:39:32.098Z"),
};
```

## Fields

| Field                        | Type                                                                                                                   | Required           | Description                                                                              |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------- |
| `annotations`                | [components.ToolAnnotations](../../models/components/toolannotations.md)                                               | :heavy_minus_sign: | Tool annotations providing behavioral hints about the tool                               |
| `canonical`                  | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)                               | :heavy_minus_sign: | The original details of a tool                                                           |
| `canonicalName`              | _string_                                                                                                               | :heavy_check_mark: | The canonical name of the tool. Will be the same as the name if there is no variation.   |
| `confirm`                    | _string_                                                                                                               | :heavy_minus_sign: | Confirmation mode for the tool                                                           |
| `confirmPrompt`              | _string_                                                                                                               | :heavy_minus_sign: | Prompt for the confirmation                                                              |
| `createdAt`                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                          | :heavy_check_mark: | The creation date of the tool.                                                           |
| `deploymentExternalMcpId`    | _string_                                                                                                               | :heavy_check_mark: | The ID of the deployments_external_mcps record                                           |
| `deploymentId`               | _string_                                                                                                               | :heavy_check_mark: | The ID of the deployment                                                                 |
| `description`                | _string_                                                                                                               | :heavy_check_mark: | Description of the tool                                                                  |
| `id`                         | _string_                                                                                                               | :heavy_check_mark: | The ID of the tool                                                                       |
| `name`                       | _string_                                                                                                               | :heavy_check_mark: | The name of the tool                                                                     |
| `oauthAuthorizationEndpoint` | _string_                                                                                                               | :heavy_minus_sign: | The OAuth authorization endpoint URL                                                     |
| `oauthRegistrationEndpoint`  | _string_                                                                                                               | :heavy_minus_sign: | The OAuth dynamic client registration endpoint URL                                       |
| `oauthScopesSupported`       | _string_[]                                                                                                             | :heavy_minus_sign: | The OAuth scopes supported by the server                                                 |
| `oauthTokenEndpoint`         | _string_                                                                                                               | :heavy_minus_sign: | The OAuth token endpoint URL                                                             |
| `oauthVersion`               | _string_                                                                                                               | :heavy_check_mark: | OAuth version: '2.1' (MCP OAuth), '2.0' (legacy), or 'none'                              |
| `projectId`                  | _string_                                                                                                               | :heavy_check_mark: | The ID of the project                                                                    |
| `registryId`                 | _string_                                                                                                               | :heavy_check_mark: | The ID of the MCP registry                                                               |
| `registryServerName`         | _string_                                                                                                               | :heavy_check_mark: | The name of the external MCP server (e.g., exa)                                          |
| `registrySpecifier`          | _string_                                                                                                               | :heavy_check_mark: | The specifier of the external MCP server (e.g., 'io.modelcontextprotocol.anonymous/exa') |
| `remoteUrl`                  | _string_                                                                                                               | :heavy_check_mark: | The URL to connect to the MCP server                                                     |
| `requiresOauth`              | _boolean_                                                                                                              | :heavy_check_mark: | Whether the external MCP server requires OAuth authentication                            |
| `schema`                     | _string_                                                                                                               | :heavy_check_mark: | JSON schema for the request                                                              |
| `schemaVersion`              | _string_                                                                                                               | :heavy_minus_sign: | Version of the schema                                                                    |
| `slug`                       | _string_                                                                                                               | :heavy_check_mark: | The slug used for tool prefixing (e.g., github)                                          |
| `summarizer`                 | _string_                                                                                                               | :heavy_minus_sign: | Summarizer for the tool                                                                  |
| `toolUrn`                    | _string_                                                                                                               | :heavy_check_mark: | The URN of this tool                                                                     |
| `transportType`              | [components.ExternalMCPToolDefinitionTransportType](../../models/components/externalmcptooldefinitiontransporttype.md) | :heavy_check_mark: | The transport type used to connect to the MCP server                                     |
| `type`                       | _string_                                                                                                               | :heavy_minus_sign: | Whether or not the tool is a proxy tool                                                  |
| `updatedAt`                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                          | :heavy_check_mark: | The last update date of the tool.                                                        |
| `variation`                  | [components.ToolVariation](../../models/components/toolvariation.md)                                                   | :heavy_minus_sign: | N/A                                                                                      |
