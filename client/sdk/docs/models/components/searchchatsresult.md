# SearchChatsResult

Result of searching chat session summaries

## Example Usage

```typescript
import { SearchChatsResult } from "@gram/client/models/components";

let value: SearchChatsResult = {
  chats: [],
  enabled: true,
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `chats`                                                            | [components.ChatSummary](../../models/components/chatsummary.md)[] | :heavy_check_mark:                                                 | List of chat session summaries                                     |
| `enabled`                                                          | *boolean*                                                          | :heavy_check_mark:                                                 | Whether tool metrics are enabled for the organization              |
| `nextCursor`                                                       | *string*                                                           | :heavy_minus_sign:                                                 | Cursor for next page                                               |