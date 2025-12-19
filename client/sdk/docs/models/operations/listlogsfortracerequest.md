# ListLogsForTraceRequest

## Example Usage

```typescript
import { ListLogsForTraceRequest } from "@gram/client/models/operations";

let value: ListLogsForTraceRequest = {
  traceId: "<id>",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `traceId`                    | *string*                     | :heavy_check_mark:           | Trace ID (32 hex characters) |
| `gramKey`                    | *string*                     | :heavy_minus_sign:           | API Key header               |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |
| `gramProject`                | *string*                     | :heavy_minus_sign:           | project header               |