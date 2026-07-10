# ToolUsage

Tool usage statistics

## Example Usage

```typescript
import { ToolUsage } from "@gram/client/models/components/toolusage.js";

let value: ToolUsage = {
  count: 129554,
  failureCount: 924683,
  successCount: 892304,
  urn: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                   |
| -------------- | -------- | ------------------ | ----------------------------- |
| `count`        | _number_ | :heavy_check_mark: | Total call count              |
| `failureCount` | _number_ | :heavy_check_mark: | Failed calls (4xx/5xx status) |
| `successCount` | _number_ | :heavy_check_mark: | Successful calls (2xx status) |
| `urn`          | _string_ | :heavy_check_mark: | Tool URN                      |
