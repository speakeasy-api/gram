# SearchChatsResult

Result of searching chat session summaries

## Example Usage

```typescript
import { SearchChatsResult } from "@gram/client/models/components/searchchatsresult.js";

let value: SearchChatsResult = {
  chats: [],
};
```

## Fields

| Field        | Type                                                               | Required           | Description                    |
| ------------ | ------------------------------------------------------------------ | ------------------ | ------------------------------ |
| `chats`      | [components.ChatSummary](../../models/components/chatsummary.md)[] | :heavy_check_mark: | List of chat session summaries |
| `nextCursor` | _string_                                                           | :heavy_minus_sign: | Cursor for next page           |
