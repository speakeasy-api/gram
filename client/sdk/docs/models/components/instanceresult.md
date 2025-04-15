# InstanceResult

## Example Usage

```typescript
import { InstanceResult } from "@gram/client/models/components";

let value: InstanceResult = {
  environment: {
    createdAt: new Date("2024-11-07T22:17:02.814Z"),
    entries: [
      {
        createdAt: new Date("2024-11-01T20:33:57.206Z"),
        name: "<value>",
        updatedAt: new Date("2024-11-07T03:49:54.674Z"),
        value: "<value>",
      },
    ],
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    slug: "<value>",
    updatedAt: new Date("2025-10-31T08:20:58.047Z"),
  },
  name: "<value>",
  tools: [
    {
      createdAt: new Date("2025-01-17T06:36:04.132Z"),
      deploymentId: "<id>",
      description: "continually fooey amid gosh arraign",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/lost+found",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2023-05-12T17:39:01.246Z"),
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