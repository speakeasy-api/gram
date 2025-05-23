# Toolset

## Example Usage

```typescript
import { Toolset } from "@gram/client/models/components";

let value: Toolset = {
  createdAt: new Date("2025-09-03T11:41:50.334Z"),
  httpTools: [
    {
      confirm: "<value>",
      createdAt: new Date("2024-05-12T19:09:07.564Z"),
      deploymentId: "<id>",
      description: "stabilise mutate gadzooks wherever pantyhose vice",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/etc/defaults",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2023-11-29T09:56:04.014Z"),
    },
  ],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-10-01T20:02:31.731Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultEnvironmentSlug`                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    |
| `httpTools`                                                                                   | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[]              | :heavy_check_mark:                                                                            | The HTTP tools in this toolset                                                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `mcpIsPublic`                                                                                 | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Whether the toolset is public in MCP                                                          |
| `mcpSlug`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `relevantEnvironmentVariables`                                                                | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The environment variables that are relevant to the toolset                                    |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |