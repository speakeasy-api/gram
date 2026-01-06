# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/client/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2026-10-23T12:10:12.732Z"),
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      promptTemplates: [],
      resourceUrns: [
        "<value 1>",
        "<value 2>",
      ],
      resources: [],
      slug: "<value>",
      toolSelectionMode: "<value>",
      toolUrns: [
        "<value 1>",
        "<value 2>",
      ],
      tools: [],
      updatedAt: new Date("2025-10-27T10:52:08.281Z"),
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `toolsets`                                                           | [components.ToolsetEntry](../../models/components/toolsetentry.md)[] | :heavy_check_mark:                                                   | The list of toolsets                                                 |