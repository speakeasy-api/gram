# ListChatsResult

## Example Usage

```typescript
import { ListChatsResult } from "@gram/client/models/components";

let value: ListChatsResult = {
  chats: [
    {
      createdAt: new Date("2024-09-05T19:49:32.564Z"),
      id: "<id>",
      numMessages: 340013,
      title: "<value>",
      updatedAt: new Date("2025-07-11T15:57:24.753Z"),
      userId: "<id>",
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `chats`                                                              | [components.ChatOverview](../../models/components/chatoverview.md)[] | :heavy_check_mark:                                                   | The list of chats                                                    |