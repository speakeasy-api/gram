# SendMessageRequestBody

## Example Usage

```typescript
import { SendMessageRequestBody } from "@gram/client/models/components/sendmessagerequestbody.js";

let value: SendMessageRequestBody = {
  assistantId: "cf6d99e1-c0d4-4233-8ab1-8bce375c0d25",
  message: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                                                                                                                           |
| ---------------- | -------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assistantId`    | _string_ | :heavy_check_mark: | The assistant to send the message to.                                                                                                                 |
| `chatId`         | _string_ | :heavy_minus_sign: | The conversation to continue (from listChats or a prior sendMessage). Omit to start a new conversation; the server mints and returns a fresh chat id. |
| `idempotencyKey` | _string_ | :heavy_minus_sign: | Stable key the client mints once per message so retries dedupe instead of enqueuing twice. A new key is generated server-side when omitted.           |
| `message`        | _string_ | :heavy_check_mark: | The user's message text.                                                                                                                              |
