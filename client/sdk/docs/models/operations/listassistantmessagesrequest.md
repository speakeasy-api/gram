# ListAssistantMessagesRequest

## Example Usage

```typescript
import { ListAssistantMessagesRequest } from "@gram/client/models/operations";

let value: ListAssistantMessagesRequest = {
  chatId: "16a798c3-8f64-4dfb-8251-d32f3fed1355",
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `chatId`                                                                     | *string*                                                                     | :heavy_check_mark:                                                           | The chat id returned by sendMessage.                                         |
| `afterSeq`                                                                   | *number*                                                                     | :heavy_minus_sign:                                                           | Return only messages with seq greater than this; omit or 0 for the full log. |
| `gramSession`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Session header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |