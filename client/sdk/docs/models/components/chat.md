# Chat

## Example Usage

```typescript
import { Chat } from "@gram/client/models/components";

let value: Chat = {
  createdAt: new Date("2024-08-21T03:55:40.546Z"),
  id: "<id>",
  messages: [
    {
      createdAt: new Date("2025-01-22T04:53:42.435Z"),
      id: "<id>",
      model: "Durango",
      role: "<value>",
    },
  ],
  numMessages: 338963,
  title: "<value>",
  updatedAt: new Date("2025-06-25T13:21:36.855Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the chat was created.                                                                    |
| `externalUserId`                                                                              | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the external user who created the chat                                              |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the chat                                                                            |
| `messages`                                                                                    | [components.ChatMessage](../../models/components/chatmessage.md)[]                            | :heavy_check_mark:                                                                            | The list of messages in the chat                                                              |
| `numMessages`                                                                                 | *number*                                                                                      | :heavy_check_mark:                                                                            | The number of messages in the chat                                                            |
| `title`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The title of the chat                                                                         |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the chat was last updated.                                                               |
| `userId`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the user who created the chat                                                       |