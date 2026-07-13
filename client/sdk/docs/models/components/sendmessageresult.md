# SendMessageResult

## Example Usage

```typescript
import { SendMessageResult } from "@gram/client/models/components/sendmessageresult.js";

let value: SendMessageResult = {
  accepted: true,
  chatId: "feb6f670-74d0-4ec3-a870-a7d66852c74a",
};
```

## Fields

| Field      | Type      | Required           | Description                                                                     |
| ---------- | --------- | ------------------ | ------------------------------------------------------------------------------- |
| `accepted` | _boolean_ | :heavy_check_mark: | Whether the message was accepted and enqueued for processing.                   |
| `chatId`   | _string_  | :heavy_check_mark: | The chat to poll for the assistant's reply.                                     |
| `threadId` | _string_  | :heavy_minus_sign: | The assistant thread the message was enqueued on, when the ingest produced one. |
