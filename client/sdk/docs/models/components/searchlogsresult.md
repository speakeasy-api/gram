# SearchLogsResult

Result of searching unified telemetry logs

## Example Usage

```typescript
import { SearchLogsResult } from "@gram/client/models/components";

let value: SearchLogsResult = {
  logs: [
    {
      attributes: "<value>",
      body: "<value>",
      id: "47253e24-ed4a-4b77-b33a-8b3a3de7248f",
      observedTimeUnixNano: 466583,
      resourceAttributes: "<value>",
      service: {
        name: "<value>",
      },
      timeUnixNano: 884724,
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `logs`                                                                           | [components.TelemetryLogRecord](../../models/components/telemetrylogrecord.md)[] | :heavy_check_mark:                                                               | List of telemetry log records                                                    |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | Cursor for next page                                                             |