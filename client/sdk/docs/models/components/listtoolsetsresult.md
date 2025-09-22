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
          id: "<id>",
          name: "<value>",
        },
      ],
      slug: "<value>",
      toolUrns: [
        "<value 1>",
      ],
      updatedAt: new Date("2024-07-07T17:12:05.835Z"),
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `toolsets`                                                           | [components.ToolsetEntry](../../models/components/toolsetentry.md)[] | :heavy_check_mark:                                                   | The list of toolsets                                                 |