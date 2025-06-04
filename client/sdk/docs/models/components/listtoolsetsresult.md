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
      slug: "<value>",
      updatedAt: new Date("2024-07-18T11:28:08.935Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |