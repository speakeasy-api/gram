# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  tools: [
    {
      canonicalName: "<value>",
      confirm: "<value>",
      createdAt: new Date("2023-11-02T08:35:12.575Z"),
      deploymentId: "<id>",
      description:
        "gaseous quaintly corral ack astride slump fooey into disposer given",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/etc",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [],
      toolUrn: "<value>",
      type: "http",
      updatedAt: new Date("2023-08-19T15:13:57.147Z"),
    },
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `nextCursor`                                                             | *string*                                                                 | :heavy_minus_sign:                                                       | The cursor to fetch results from                                         |
| `tools`                                                                  | *components.Tool*[]                                                      | :heavy_check_mark:                                                       | The list of tools (polymorphic union of HTTP tools and prompt templates) |