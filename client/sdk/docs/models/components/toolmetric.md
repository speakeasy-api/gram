# ToolMetric

Aggregated metrics for a single tool

## Example Usage

```typescript
import { ToolMetric } from "@gram/client/models/components/toolmetric.js";

let value: ToolMetric = {
  avgLatencyMs: 1902.53,
  callCount: 124409,
  failureCount: 122377,
  failureRate: 1499.69,
  gramUrn: "<value>",
  successCount: 638944,
};
```

## Fields

| Field          | Type     | Required           | Description                     |
| -------------- | -------- | ------------------ | ------------------------------- |
| `avgLatencyMs` | _number_ | :heavy_check_mark: | Average latency in milliseconds |
| `callCount`    | _number_ | :heavy_check_mark: | Total number of calls           |
| `failureCount` | _number_ | :heavy_check_mark: | Number of failed calls          |
| `failureRate`  | _number_ | :heavy_check_mark: | Failure rate (0.0 to 1.0)       |
| `gramUrn`      | _string_ | :heavy_check_mark: | Tool URN                        |
| `successCount` | _number_ | :heavy_check_mark: | Number of successful calls      |
