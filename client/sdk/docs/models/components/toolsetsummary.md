# ToolsetSummary

A lightweight summary of a toolset, containing only the fields needed for org-level listing (e.g. RBAC UI).

## Example Usage

```typescript
import { ToolsetSummary } from "@gram/client/models/components/toolsetsummary.js";

let value: ToolsetSummary = {
  createdAt: new Date("2025-10-31T15:18:36.962Z"),
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  toolSelectionMode: "<value>",
  tools: [
    {
      id: "<id>",
      name: "<value>",
      toolUrn: "<value>",
      type: "http",
    },
  ],
  updatedAt: new Date("2026-08-21T02:49:20.067Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultEnvironmentSlug`                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `mcpEnabled`                                                                                  | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Whether the toolset is enabled for MCP                                                        |
| `mcpIsPublic`                                                                                 | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Whether the toolset is public in MCP                                                          |
| `mcpSlug`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `toolSelectionMode`                                                                           | *string*                                                                                      | :heavy_check_mark:                                                                            | The mode to use for tool selection                                                            |
| `tools`                                                                                       | [components.ToolEntry](../../models/components/toolentry.md)[]                                | :heavy_check_mark:                                                                            | The tools in this toolset                                                                     |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |