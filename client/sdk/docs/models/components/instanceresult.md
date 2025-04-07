# InstanceResult

## Example Usage

```typescript
import { InstanceResult } from "@gram/sdk/models/components";

let value: InstanceResult = {
  environment: {
    createdAt: new Date("2023-10-17T18:07:30.469Z"),
    entries: [
      {
        createdAt: new Date("2025-02-20T04:41:45.906Z"),
        name: "<value>",
        updatedAt: new Date("2024-03-10T22:10:58.239Z"),
        value: "<value>",
      },
    ],
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    slug: "<value>",
    updatedAt: new Date("2024-08-18T10:47:36.935Z"),
  },
  name: "<value>",
  tools: [
    {
      createdAt: new Date("2024-04-15T03:02:21.323Z"),
      deploymentId: "<id>",
      description: "archive since murky dependency syringe instantly",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/usr/local/bin",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2024-05-13T03:29:42.791Z"),
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `description`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | The description of the toolset                                                   |
| `environment`                                                                    | [components.Environment](../../models/components/environment.md)                 | :heavy_check_mark:                                                               | Model representing an environment                                                |
| `name`                                                                           | *string*                                                                         | :heavy_check_mark:                                                               | The name of the toolset                                                          |
| `relevantEnvironmentVariables`                                                   | *string*[]                                                                       | :heavy_minus_sign:                                                               | The environment variables that are relevant to the toolset                       |
| `tools`                                                                          | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[] | :heavy_check_mark:                                                               | The list of tools                                                                |