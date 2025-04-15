# ToolsetDetails

## Example Usage

```typescript
import { ToolsetDetails } from "@gram/client/models/components";

let value: ToolsetDetails = {
  createdAt: new Date("2024-04-17T20:01:49.892Z"),
  httpTools: [
    {
      createdAt: new Date("2025-09-09T14:45:41.967Z"),
      deploymentId: "<id>",
      description: "remand whether seriously huzzah immediately",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/var/log",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2025-02-11T00:54:59.142Z"),
    },
  ],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2023-02-04T21:29:28.328Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultEnvironmentSlug`                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | The slug of the environment to use as the default for the toolset                             |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    |
| `httpTools`                                                                                   | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[]              | :heavy_check_mark:                                                                            | The HTTP tools in this toolset                                                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `relevantEnvironmentVariables`                                                                | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The environment variables that are relevant to the toolset                                    |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug of the toolset                                                                       |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |