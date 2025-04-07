# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/sdk/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2025-09-20T12:21:57.178Z"),
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2025-10-23T12:10:12.732Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |