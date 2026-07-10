# ListToolUsageTracesResult

Result of listing target-aware MCP and tool usage traces

## Example Usage

```typescript
import { ListToolUsageTracesResult } from "@gram/client/models/components/listtoolusagetracesresult.js";

let value: ListToolUsageTracesResult = {
  traces: [],
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `nextCursor`                                                                           | *string*                                                                               | :heavy_minus_sign:                                                                     | Cursor for next page                                                                   |
| `traces`                                                                               | [components.ToolUsageTraceSummary](../../models/components/toolusagetracesummary.md)[] | :heavy_check_mark:                                                                     | Target-aware tool usage trace rows                                                     |