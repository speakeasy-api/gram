# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  tools: [
    {
      canonicalName: "<value>",
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
        "<value 1>",
      ],
      toolType: "http",
      updatedAt: new Date("2025-03-13T06:28:48.216Z"),
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The cursor to fetch results from                                                 |
| `tools`                                                                          | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[] | :heavy_check_mark:                                                               | The list of tools                                                                |