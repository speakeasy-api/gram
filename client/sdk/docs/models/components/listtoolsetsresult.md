# ListToolsetsResult

## Example Usage

```typescript
import { ListToolsetsResult } from "@gram/sdk/models/components";

let value: ListToolsetsResult = {
  toolsets: [
    {
      createdAt: new Date("2023-01-13T20:07:38.173Z"),
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      projectId: "<id>",
      slug: "<value>",
      updatedAt: new Date("2024-01-30T12:51:46.829Z"),
    },
  ],
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `toolsets`                                                 | [components.Toolset](../../models/components/toolset.md)[] | :heavy_check_mark:                                         | The list of toolsets                                       |