# ListLogsForTraceResult

Result of listing logs for a trace

## Example Usage

```typescript
import { ListLogsForTraceResult } from "@gram/client/models/components";

let value: ListLogsForTraceResult = {
  logs: [],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `logs`                                                                           | [components.TelemetryLogRecord](../../models/components/telemetrylogrecord.md)[] | :heavy_check_mark:                                                               | List of telemetry log records for this trace                                     |