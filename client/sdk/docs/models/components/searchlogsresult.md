# SearchLogsResult

Result of searching telemetry logs

## Example Usage

```typescript
import { SearchLogsResult } from "@gram/client/models/components";

let value: SearchLogsResult = {
  enabled: false,
  logs: [],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `enabled`                                                                        | *boolean*                                                                        | :heavy_check_mark:                                                               | Whether tool metrics are enabled for the organization                            |
| `logs`                                                                           | [components.TelemetryLogRecord](../../models/components/telemetrylogrecord.md)[] | :heavy_check_mark:                                                               | List of telemetry log records                                                    |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | Cursor for next page                                                             |