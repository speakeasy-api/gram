# ListChatsResult

## Example Usage

```typescript
import { ListChatsResult } from "@gram/client/models/components";

let value: ListChatsResult = {
  chats: [
    {
      createdAt: new Date("2025-01-07T15:42:34.974Z"),
      id: "<id>",
      numMessages: 841848,
      title: "<value>",
      updatedAt: new Date("2025-09-09T09:20:23.777Z"),
      userId: "<id>",
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `chats`                                                              | [components.ChatOverview](../../models/components/chatoverview.md)[] | :heavy_check_mark:                                                   | The list of chats                                                    |