# ToolUsage

Tool usage statistics

## Example Usage

```typescript
import { ToolUsage } from "@gram/client/models/components";

let value: ToolUsage = {
  count: 129554,
  failureCount: 924683,
  successCount: 892304,
  urn: "<value>",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `count`                       | *number*                      | :heavy_check_mark:            | Total call count              |
| `failureCount`                | *number*                      | :heavy_check_mark:            | Failed calls (4xx/5xx status) |
| `successCount`                | *number*                      | :heavy_check_mark:            | Successful calls (2xx status) |
| `urn`                         | *string*                      | :heavy_check_mark:            | Tool URN                      |