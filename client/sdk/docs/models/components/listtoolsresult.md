# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  tools: [
    {
      createdAt: new Date("2024-07-22T19:54:53.930Z"),
      deploymentId: "<id>",
      id: "<id>",
      name: "<value>",
      openapiv3DocumentId: "<id>",
      summary: "<value>",
    },
  ],
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `nextCursor`                                                   | *string*                                                       | :heavy_minus_sign:                                             | The cursor to fetch results from                               |
| `tools`                                                        | [components.ToolEntry](../../models/components/toolentry.md)[] | :heavy_check_mark:                                             | The list of tools                                              |