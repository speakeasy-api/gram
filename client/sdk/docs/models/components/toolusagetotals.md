# ToolUsageTotals

Target-aware MCP and tool usage totals

## Example Usage

```typescript
import { ToolUsageTotals } from "@gram/client/models/components/toolusagetotals.js";

let value: ToolUsageTotals = {
  eventCount: 814220,
  failureCount: 314062,
  failureRate: 2406.01,
  successCount: 516527,
  uniqueTargets: 598062,
  uniqueTools: 72086,
  uniqueUsers: 91398,
};
```

## Fields

| Field           | Type     | Required           | Description                                         |
| --------------- | -------- | ------------------ | --------------------------------------------------- |
| `eventCount`    | _number_ | :heavy_check_mark: | Total number of tool usage events                   |
| `failureCount`  | _number_ | :heavy_check_mark: | Number of failed tool usage events                  |
| `failureRate`   | _number_ | :heavy_check_mark: | Fraction of completed tool usage events that failed |
| `successCount`  | _number_ | :heavy_check_mark: | Number of successful tool usage events              |
| `uniqueTargets` | _number_ | :heavy_check_mark: | Number of distinct usage targets observed           |
| `uniqueTools`   | _number_ | :heavy_check_mark: | Number of distinct tools observed                   |
| `uniqueUsers`   | _number_ | :heavy_check_mark: | Number of distinct user identities observed         |
