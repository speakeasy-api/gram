# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/client/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2023-06-30T12:06:54.333Z"),
      httpTools: [
        {
          createdAt: new Date("2024-11-12T03:22:04.343Z"),
          deploymentId: "<id>",
          description: "general astride boohoo without godparent finally aside",
          httpMethod: "<value>",
          id: "<id>",
          name: "<value>",
          path: "/rescue",
          projectId: "<id>",
          schema: "<value>",
          summary: "<value>",
          tags: [
            "<value>",
          ],
          updatedAt: new Date("2023-01-29T03:01:55.688Z"),
        },
      ],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2023-08-16T09:17:34.312Z"),
    },
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `toolsets`                                                               | [components.ToolsetDetails](../../models/components/toolsetdetails.md)[] | :heavy_check_mark:                                                       | The list of toolsets                                                     |