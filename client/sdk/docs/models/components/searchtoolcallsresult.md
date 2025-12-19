# SearchToolCallsResult

Result of searching tool call summaries

## Example Usage

```typescript
import { SearchToolCallsResult } from "@gram/client/models/components";

let value: SearchToolCallsResult = {
  toolCalls: [],
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `nextCursor`                                                               | *string*                                                                   | :heavy_minus_sign:                                                         | Cursor for next page                                                       |
| `toolCalls`                                                                | [components.ToolCallSummary](../../models/components/toolcallsummary.md)[] | :heavy_check_mark:                                                         | List of tool call summaries                                                |