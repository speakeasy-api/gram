# ChatMessage

## Example Usage

```typescript
import { ChatMessage } from "@gram/client/models/components";

let value: ChatMessage = {
  createdAt: new Date("2025-05-23T00:28:17.065Z"),
  id: "<id>",
  model: "Fiesta",
  role: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `content`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | The content of the message                                                                    |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the message was created.                                                                 |
| `externalUserId`                                                                              | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the external user who created the message                                           |
| `finishReason`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | The finish reason of the message                                                              |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the message                                                                         |
| `model`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The model that generated the message                                                          |
| `role`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The role of the message                                                                       |
| `toolCallId`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | The tool call ID of the message                                                               |
| `toolCalls`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The tool calls in the message as a JSON blob                                                  |
| `userId`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the user who created the message                                                    |