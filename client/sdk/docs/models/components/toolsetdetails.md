# ToolsetDetails

## Example Usage

```typescript
import { ToolsetDetails } from "@gram/sdk/models/components";

let value: ToolsetDetails = {
  createdAt: new Date("2024-10-26T14:34:01.576Z"),
  httpTools: [
    {
      createdAt: new Date("2023-01-22T00:51:28.398Z"),
      description: "outfit hidden remand whether seriously",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/usr/sbin",
      securityType: "<value>",
      serverEnvVar: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2025-12-05T04:07:03.604Z"),
    },
  ],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-07-26T23:03:04.026Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultEnvironmentId`                                                                        | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the environment to use as the default for the toolset                               |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    |
| `httpTools`                                                                                   | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[]              | :heavy_check_mark:                                                                            | The HTTP tools in this toolset                                                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug of the toolset                                                                       |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |