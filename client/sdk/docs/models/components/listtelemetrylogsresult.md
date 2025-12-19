# ListTelemetryLogsResult

Result of listing unified telemetry logs

## Example Usage

```typescript
import { ListTelemetryLogsResult } from "@gram/client/models/components";

let value: ListTelemetryLogsResult = {
  logs: [],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `logs`                                                                           | [components.TelemetryLogRecord](../../models/components/telemetrylogrecord.md)[] | :heavy_check_mark:                                                               | List of telemetry log records                                                    |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | Cursor for next page                                                             |