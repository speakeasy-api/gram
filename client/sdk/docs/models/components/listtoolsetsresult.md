# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/client/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2025-10-23T12:10:12.732Z"),
      httpTools: [],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      promptTemplates: [
        {
          createdAt: new Date("2024-04-18T10:37:41.805Z"),
          engine: "mustache",
          historyId: "<id>",
          id: "<id>",
          kind: "higher_order_tool",
          name: "<value>",
          prompt: "<value>",
          toolsHint: [],
          updatedAt: new Date("2024-10-27T10:52:08.281Z"),
        },
      ],
      slug: "<value>",
      updatedAt: new Date("2025-12-23T11:52:23.236Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |