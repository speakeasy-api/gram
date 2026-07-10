# ListRiskResultsByChatResult

## Example Usage

```typescript
import { ListRiskResultsByChatResult } from "@gram/client/models/components/listriskresultsbychatresult.js";

let value: ListRiskResultsByChatResult = {
  chats: [],
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `chats`                                                                    | [components.RiskChatSummary](../../models/components/riskchatsummary.md)[] | :heavy_check_mark:                                                         | Risk results grouped by chat.                                              |
| `nextCursor`                                                               | *string*                                                                   | :heavy_minus_sign:                                                         | Cursor for the next page of results.                                       |