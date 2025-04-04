# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/sdk/models/components";

let value: ListToolsResult = {
  tools: [
    {
      createdAt: new Date("2025-10-31T08:20:58.047Z"),
      description: "cuckoo canter even along rim woot minus apropos",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/srv",
      securityType: "<value>",
      serverEnvVar: "<value>",
      tags: [
        "<value>",
      ],
      updatedAt: new Date("2023-01-23T00:54:32.021Z"),
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The cursor to fetch results from                                                 |
| `tools`                                                                          | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[] | :heavy_check_mark:                                                               | The list of tools                                                                |