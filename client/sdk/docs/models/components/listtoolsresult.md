# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  tools: [
    {
      createdAt: new Date("2024-09-19T04:30:50.868Z"),
      deploymentId: "<id>",
      description: "aha well-to-do below outrun",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/Users",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2024-12-15T16:55:27.891Z"),
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The cursor to fetch results from                                                 |
| `tools`                                                                          | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[] | :heavy_check_mark:                                                               | The list of tools                                                                |