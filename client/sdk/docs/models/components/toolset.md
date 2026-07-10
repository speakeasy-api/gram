# Toolset

## Example Usage

```typescript
import { Toolset } from "@gram/client/models/components/toolset.js";

let value: Toolset = {
  accountType: "<value>",
  createdAt: new Date("2026-09-03T11:41:50.334Z"),
  id: "<id>",
  name: "<value>",
  oauthEnablementMetadata: {
    oauth2SecurityCount: 454195,
  },
  organizationId: "<id>",
  projectId: "<id>",
  promptTemplates: [],
  resourceUrns: [],
  resources: [],
  slug: "<value>",
  toolSelectionMode: "<value>",
  toolUrns: ["<value 1>"],
  tools: [{}],
  toolsetVersion: 606302,
  updatedAt: new Date("2025-09-30T12:50:02.116Z"),
};
```

## Fields

| Field                          | Type                                                                                               | Required           | Description                                                                                                                                                 |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `accountType`                  | _string_                                                                                           | :heavy_check_mark: | The account type of the organization                                                                                                                        |
| `createdAt`                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_check_mark: | When the toolset was created.                                                                                                                               |
| `customDomainId`               | _string_                                                                                           | :heavy_minus_sign: | The ID of the custom domain to use for the toolset                                                                                                          |
| `defaultEnvironmentSlug`       | _string_                                                                                           | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource.                                                                                             |
| `description`                  | _string_                                                                                           | :heavy_minus_sign: | Description of the toolset                                                                                                                                  |
| `externalMcpHeaderDefinitions` | [components.ExternalMCPHeaderDefinition](../../models/components/externalmcpheaderdefinition.md)[] | :heavy_minus_sign: | The external MCP header definitions that are relevant to the toolset                                                                                        |
| `externalOauthServer`          | [components.ExternalOAuthServer](../../models/components/externaloauthserver.md)                   | :heavy_minus_sign: | N/A                                                                                                                                                         |
| `functionEnvironmentVariables` | [components.FunctionEnvironmentVariable](../../models/components/functionenvironmentvariable.md)[] | :heavy_minus_sign: | The function environment variables that are relevant to the toolset                                                                                         |
| `id`                           | _string_                                                                                           | :heavy_check_mark: | The ID of the toolset                                                                                                                                       |
| `mcpEnabled`                   | _boolean_                                                                                          | :heavy_minus_sign: | Whether the toolset is enabled for MCP                                                                                                                      |
| `mcpIsPublic`                  | _boolean_                                                                                          | :heavy_minus_sign: | Whether the toolset is public in MCP                                                                                                                        |
| `mcpSlug`                      | _string_                                                                                           | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource.                                                                                             |
| `name`                         | _string_                                                                                           | :heavy_check_mark: | The name of the toolset                                                                                                                                     |
| `oauthEnablementMetadata`      | [components.OAuthEnablementMetadata](../../models/components/oauthenablementmetadata.md)           | :heavy_check_mark: | N/A                                                                                                                                                         |
| `oauthProxyServer`             | [components.OAuthProxyServer](../../models/components/oauthproxyserver.md)                         | :heavy_minus_sign: | N/A                                                                                                                                                         |
| `organizationId`               | _string_                                                                                           | :heavy_check_mark: | The organization ID this toolset belongs to                                                                                                                 |
| `origin`                       | [components.ToolsetOrigin](../../models/components/toolsetorigin.md)                               | :heavy_minus_sign: | N/A                                                                                                                                                         |
| `projectId`                    | _string_                                                                                           | :heavy_check_mark: | The project ID this toolset belongs to                                                                                                                      |
| `promptTemplates`              | [components.PromptTemplate](../../models/components/prompttemplate.md)[]                           | :heavy_check_mark: | The prompt templates in this toolset -- Note: these are actual prompts, as in MCP prompts                                                                   |
| `resourceUrns`                 | _string_[]                                                                                         | :heavy_check_mark: | The resource URNs in this toolset                                                                                                                           |
| `resources`                    | [components.Resource](../../models/components/resource.md)[]                                       | :heavy_check_mark: | The resources in this toolset                                                                                                                               |
| `securityVariables`            | [components.SecurityVariable](../../models/components/securityvariable.md)[]                       | :heavy_minus_sign: | The security variables that are relevant to the toolset                                                                                                     |
| `serverVariables`              | [components.ServerVariable](../../models/components/servervariable.md)[]                           | :heavy_minus_sign: | The server variables that are relevant to the toolset                                                                                                       |
| `slug`                         | _string_                                                                                           | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource.                                                                                             |
| `toolSelectionMode`            | _string_                                                                                           | :heavy_check_mark: | The mode to use for tool selection                                                                                                                          |
| `toolUrns`                     | _string_[]                                                                                         | :heavy_check_mark: | The tool URNs in this toolset                                                                                                                               |
| `toolVariationsGroupId`        | _string_                                                                                           | :heavy_minus_sign: | The id of the tool variations group enabling MCP tool filtering for this toolset. Set via toolsets.setToolVariationsGroup; null when filtering is disabled. |
| `tools`                        | [components.Tool](../../models/components/tool.md)[]                                               | :heavy_check_mark: | The tools in this toolset                                                                                                                                   |
| `toolsetVersion`               | _number_                                                                                           | :heavy_check_mark: | The version of the toolset (will be 0 if none exists)                                                                                                       |
| `updatedAt`                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_check_mark: | When the toolset was last updated.                                                                                                                          |
| `userSessionIssuerId`          | _string_                                                                                           | :heavy_minus_sign: | The id of the user_session_issuer wired to this toolset. Set via toolsets.setUserSessionIssuer; null when no USI is linked.                                 |
| `userSessionIssuerSlug`        | _string_                                                                                           | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource.                                                                                             |
