# ListSessionsResult

Result of listing org-scoped chat sessions

## Example Usage

```typescript
import { ListSessionsResult } from "@gram/client/models/components/listsessionsresult.js";

let value: ListSessionsResult = {
  sessions: [
    {
      durationSeconds: 8530,
      endTimeUnixNano: "<value>",
      gramChatId: "<id>",
      messageCount: 991313,
      projectId: "<id>",
      startTimeUnixNano: "<value>",
      status: "error",
      toolCallCount: 277892,
      totalCost: 5632.43,
      totalInputTokens: 65132,
      totalOutputTokens: 231221,
      totalTokens: 100675,
    },
  ],
};
```

## Fields

| Field        | Type                                                                     | Required           | Description                    |
| ------------ | ------------------------------------------------------------------------ | ------------------ | ------------------------------ |
| `nextCursor` | _string_                                                                 | :heavy_minus_sign: | Cursor for next page           |
| `sessions`   | [components.SessionSummary](../../models/components/sessionsummary.md)[] | :heavy_check_mark: | List of chat session summaries |
