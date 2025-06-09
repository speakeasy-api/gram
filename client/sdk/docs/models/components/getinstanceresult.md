# GetInstanceResult

## Example Usage

```typescript
import { GetInstanceResult } from "@gram/client/models/components";

let value: GetInstanceResult = {
  environment: {
    createdAt: new Date("2025-09-30T14:15:42.248Z"),
    entries: [
      {
        createdAt: new Date("2025-07-08T12:50:08.798Z"),
        name: "<value>",
        updatedAt: new Date("2023-07-28T21:14:20.018Z"),
        value: "<value>",
      },
    ],
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    projectId: "<id>",
    slug: "<value>",
    updatedAt: new Date("2025-07-10T05:17:09.093Z"),
  },
  name: "<value>",
  tools: [
    {
      confirm: "<value>",
      createdAt: new Date("2023-08-05T21:57:17.116Z"),
      deploymentId: "<id>",
      description: "sheepishly like to fooey why",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/etc/ppp",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2024-04-28T02:23:08.344Z"),
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