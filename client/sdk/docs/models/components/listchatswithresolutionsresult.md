# ListChatsWithResolutionsResult

Result of listing chats with resolutions

## Example Usage

```typescript
import { ListChatsWithResolutionsResult } from "@gram/client/models/components";

let value: ListChatsWithResolutionsResult = {
  chats: [
    {
      createdAt: new Date("2026-08-01T13:40:00.915Z"),
      id: "<id>",
      lastMessageTimestamp: new Date("2024-11-03T14:36:42.593Z"),
      numMessages: 415344,
      resolutions: [],
      title: "<value>",
      updatedAt: new Date("2024-06-27T13:40:33.756Z"),
    },
  ],
  total: 480351,
};
```

## Fields

| Field   | Type                                                                                               | Required           | Description                               |
| ------- | -------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------- |
| `chats` | [components.ChatOverviewWithResolutions](../../models/components/chatoverviewwithresolutions.md)[] | :heavy_check_mark: | List of chats with resolutions            |
| `total` | _number_                                                                                           | :heavy_check_mark: | Total number of chats (before pagination) |
