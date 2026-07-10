# ListAssistantMemoriesRequest

## Example Usage

```typescript
import { ListAssistantMemoriesRequest } from "@gram/client/models/operations/listassistantmemories.js";

let value: ListAssistantMemoriesRequest = {
  assistantId: "2a1b6f01-9559-4490-86d4-914f1f8a5837",
};
```

## Fields

| Field                                      | Type                                       | Required                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `assistantId`                              | *string*                                   | :heavy_check_mark:                         | The assistant ID.                          |
| `tags`                                     | *string*[]                                 | :heavy_minus_sign:                         | Optional tags to filter memories by.       |
| `includeDeleted`                           | *boolean*                                  | :heavy_minus_sign:                         | Whether to include soft-deleted memories.  |
| `cursor`                                   | *string*                                   | :heavy_minus_sign:                         | The cursor to fetch results from.          |
| `limit`                                    | *number*                                   | :heavy_minus_sign:                         | The number of memories to return per page. |
| `gramSession`                              | *string*                                   | :heavy_minus_sign:                         | Session header                             |
| `gramProject`                              | *string*                                   | :heavy_minus_sign:                         | project header                             |