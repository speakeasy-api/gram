# ToolsetEntry

## Example Usage

```typescript
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";

let value: ToolsetEntry = {
  createdAt: new Date("2025-06-19T11:54:18.705Z"),
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  promptTemplates: [],
  resourceUrns: [],
  resources: [],
  slug: "<value>",
  toolSelectionMode: "<value>",
  toolUrns: [],
  tools: [
    {
      id: "<id>",
      name: "<value>",
      toolUrn: "<value>",
      type: "http",
    },
  ],
  updatedAt: new Date("2026-10-29T00:12:43.112Z"),
};
```

## Fields

| Field                          | Type                                                                                               | Required           | Description                                                                               |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------- |
| `createdAt`                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_check_mark: | When the toolset was created.                                                             |
| `customDomainId`               | _string_                                                                                           | :heavy_minus_sign: | The ID of the custom domain to use for the toolset                                        |
| `defaultEnvironmentSlug`       | _string_                                                                                           | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource.                           |
| `description`                  | _string_                                                                                           | :heavy_minus_sign: | Description of the toolset                                                                |
| `externalMcpHeaderDefinitions` | [components.ExternalMCPHeaderDefinition](../../models/components/externalmcpheaderdefinition.md)[] | :heavy_minus_sign: | The external MCP header definitions that are relevant to the toolset                      |
| `functionEnvironmentVariables` | [components.FunctionEnvironmentVariable](../../models/components/functionenvironmentvariable.md)[] | :heavy_minus_sign: | The function environment variables that are relevant to the toolset                       |
| `id`                           | _string_                                                                                           | :heavy_check_mark: | The ID of the toolset                                                                     |
| `mcpEnabled`                   | _boolean_                                                                                          | :heavy_minus_sign: | Whether the toolset is enabled for MCP                                                    |
| `mcpIsPublic`                  | _boolean_                                                                                          | :heavy_minus_sign: | Whether the toolset is public in MCP                                                      |
| `mcpSlug`                      | _string_                                                                                           | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource.                           |
| `name`                         | _string_                                                                                           | :heavy_check_mark: | The name of the toolset                                                                   |
| `organizationId`               | _string_                                                                                           | :heavy_check_mark: | The organization ID this toolset belongs to                                               |
| `origin`                       | [components.ToolsetOrigin](../../models/components/toolsetorigin.md)                               | :heavy_minus_sign: | N/A                                                                                       |
| `projectId`                    | _string_                                                                                           | :heavy_check_mark: | The project ID this toolset belongs to                                                    |
| `promptTemplates`              | [components.PromptTemplateEntry](../../models/components/prompttemplateentry.md)[]                 | :heavy_check_mark: | The prompt templates in this toolset -- Note: these are actual prompts, as in MCP prompts |
| `resourceUrns`                 | _string_[]                                                                                         | :heavy_check_mark: | The resource URNs in this toolset                                                         |
| `resources`                    | [components.ResourceEntry](../../models/components/resourceentry.md)[]                             | :heavy_check_mark: | The resources in this toolset                                                             |
| `securityVariables`            | [components.SecurityVariable](../../models/components/securityvariable.md)[]                       | :heavy_minus_sign: | The security variables that are relevant to the toolset                                   |
| `serverVariables`              | [components.ServerVariable](../../models/components/servervariable.md)[]                           | :heavy_minus_sign: | The server variables that are relevant to the toolset                                     |
| `slug`                         | _string_                                                                                           | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource.                           |
| `toolSelectionMode`            | _string_                                                                                           | :heavy_check_mark: | The mode to use for tool selection                                                        |
| `toolUrns`                     | _string_[]                                                                                         | :heavy_check_mark: | The tool URNs in this toolset                                                             |
| `tools`                        | [components.ToolEntry](../../models/components/toolentry.md)[]                                     | :heavy_check_mark: | The tools in this toolset                                                                 |
| `updatedAt`                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)      | :heavy_check_mark: | When the toolset was last updated.                                                        |
