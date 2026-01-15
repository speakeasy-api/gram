# PublishingStatus

Publishing status comparing current vs published toolset versions

## Example Usage

```typescript
import { PublishingStatus } from "@gram/client/models/components";

let value: PublishingStatus = {
  currentVersion: 128866,
  hasChanges: true,
  isPublished: true,
  publishedVersion: 258768,
  resourceChanges: {
    added: [
      "<value 1>",
      "<value 2>",
    ],
    addedCount: 940840,
    removed: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    removedCount: 920093,
  },
  toolChanges: {
    added: [],
    addedCount: 675045,
    removed: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    removedCount: 747526,
  },
  toolsetId: "<id>",
  toolsetName: "<value>",
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `currentVersion`                                                         | *number*                                                                 | :heavy_check_mark:                                                       | The current toolset version number                                       |
| `hasChanges`                                                             | *boolean*                                                                | :heavy_check_mark:                                                       | Whether there are unpublished changes                                    |
| `isPublished`                                                            | *boolean*                                                                | :heavy_check_mark:                                                       | Whether the toolset has ever been published                              |
| `publishedVersion`                                                       | *number*                                                                 | :heavy_check_mark:                                                       | The published toolset version number (0 if never published)              |
| `resourceChanges`                                                        | [components.ResourceChanges](../../models/components/resourcechanges.md) | :heavy_check_mark:                                                       | Summary of resource changes between published and current versions       |
| `toolChanges`                                                            | [components.ToolChanges](../../models/components/toolchanges.md)         | :heavy_check_mark:                                                       | Summary of tool changes between published and current versions           |
| `toolsetId`                                                              | *string*                                                                 | :heavy_check_mark:                                                       | The ID of the toolset                                                    |
| `toolsetName`                                                            | *string*                                                                 | :heavy_check_mark:                                                       | The name of the toolset                                                  |