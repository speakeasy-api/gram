# ListChatsResult

## Example Usage

```typescript
import { ListChatsResult } from "@gram/client/models/components/listchatsresult.js";

let value: ListChatsResult = {
  chats: [
    {
      createdAt: new Date("2025-01-07T15:42:34.974Z"),
      id: "<id>",
      lastMessageTimestamp: new Date("2026-07-11T15:57:24.753Z"),
      numMessages: 563311,
      title: "<value>",
      updatedAt: new Date("2024-10-28T17:19:24.210Z"),
    },
  ],
  total: 410034,
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `chats`                                                              | [components.ChatOverview](../../models/components/chatoverview.md)[] | :heavy_check_mark:                                                   | The list of chats                                                    |
| `total`                                                              | *number*                                                             | :heavy_check_mark:                                                   | Total number of chats (before pagination)                            |