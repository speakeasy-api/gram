# ToolsetEntry

## Example Usage

```typescript
import { ToolsetEntry } from "@gram/client/models/components";

let value: ToolsetEntry = {
  createdAt: new Date("2024-06-19T11:54:18.705Z"),
  httpTools: [],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  promptTemplates: [],
  slug: "<value>",
  updatedAt: new Date("2023-12-06T06:26:11.959Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `customDomainId`                                                                              | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the custom domain to use for the toolset                                            |
| `defaultEnvironmentSlug`                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    |
| `httpTools`                                                                                   | [components.HTTPToolDefinitionEntry](../../models/components/httptooldefinitionentry.md)[]    | :heavy_check_mark:                                                                            | The HTTP tools in this toolset                                                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `mcpIsPublic`                                                                                 | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Whether the toolset is public in MCP                                                          |
| `mcpSlug`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `promptTemplates`                                                                             | [components.PromptTemplateEntry](../../models/components/prompttemplateentry.md)[]            | :heavy_check_mark:                                                                            | The prompt templates in this toolset                                                          |
| `securityVariables`                                                                           | [components.SecurityVariable](../../models/components/securityvariable.md)[]                  | :heavy_minus_sign:                                                                            | The security variables that are relevant to the toolset                                       |
| `serverVariables`                                                                             | [components.ServerVariable](../../models/components/servervariable.md)[]                      | :heavy_minus_sign:                                                                            | The server variables that are relevant to the toolset                                         |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |