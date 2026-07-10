# ChatMessage

## Example Usage

```typescript
import { ChatMessage } from "@gram/client/models/components/chatmessage.js";

let value: ChatMessage = {
  createdAt: new Date("2025-05-23T00:28:17.065Z"),
  generation: 499504,
  id: "<id>",
  model: "Model Y",
  role: "<value>",
  seq: 46781,
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                                                                                                                                                                                               |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `content`        | _any_                                                                                         | :heavy_minus_sign: | The content of the message — string for plain text, array for multimodal/tool-call content parts, null for assistant messages that only carry tool_calls                                                                                                  |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the message was created.                                                                                                                                                                                                                             |
| `externalUserId` | _string_                                                                                      | :heavy_minus_sign: | The ID of the external user who created the message                                                                                                                                                                                                       |
| `finishReason`   | _string_                                                                                      | :heavy_minus_sign: | The finish reason of the message                                                                                                                                                                                                                          |
| `generation`     | _number_                                                                                      | :heavy_check_mark: | Conversation generation — bumps on compaction or edit divergence                                                                                                                                                                                          |
| `id`             | _string_                                                                                      | :heavy_check_mark: | The ID of the message                                                                                                                                                                                                                                     |
| `isRisk`         | _boolean_                                                                                     | :heavy_minus_sign: | Present only in `risk_only` mode: true when this message has an active risk finding, false for the surrounding-context messages padded around it.                                                                                                         |
| `model`          | _string_                                                                                      | :heavy_check_mark: | The model that generated the message                                                                                                                                                                                                                      |
| `promptId`       | _string_                                                                                      | :heavy_minus_sign: | The agent prompt/turn ID associated with this message, when available.                                                                                                                                                                                    |
| `role`           | _string_                                                                                      | :heavy_check_mark: | The role of the message                                                                                                                                                                                                                                   |
| `seq`            | _number_                                                                                      | :heavy_check_mark: | Monotonic sequence number of the message. Strictly increasing within a chat; use it as the keyset cursor for `before_seq`/`after_seq` pagination. Not contiguous (the sequence is shared across chats), so do not infer gaps from arithmetic differences. |
| `toolCallId`     | _string_                                                                                      | :heavy_minus_sign: | The tool call ID of the message                                                                                                                                                                                                                           |
| `toolCalls`      | _string_                                                                                      | :heavy_minus_sign: | The tool calls in the message as a JSON blob                                                                                                                                                                                                              |
| `userId`         | _string_                                                                                      | :heavy_minus_sign: | The ID of the user who created the message                                                                                                                                                                                                                |
