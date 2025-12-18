# TelemetryLogRecord

OpenTelemetry log record

## Example Usage

```typescript
import { TelemetryLogRecord } from "@gram/client/models/components";

let value: TelemetryLogRecord = {
  attributes: "<value>",
  body: "<value>",
  id: "b0b3b922-e157-405a-99f8-a1e7e6b0a753",
  observedTimeUnixNano: 44372,
  resourceAttributes: "<value>",
  service: {
    name: "<value>",
  },
  timeUnixNano: 773299,
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `attributes`                                                     | *any*                                                            | :heavy_check_mark:                                               | Log attributes as JSON object                                    |
| `body`                                                           | *string*                                                         | :heavy_check_mark:                                               | The primary log message                                          |
| `id`                                                             | *string*                                                         | :heavy_check_mark:                                               | Log record ID                                                    |
| `observedTimeUnixNano`                                           | *number*                                                         | :heavy_check_mark:                                               | Unix time in nanoseconds when event was observed                 |
| `resourceAttributes`                                             | *any*                                                            | :heavy_check_mark:                                               | Resource attributes as JSON object                               |
| `service`                                                        | [components.ServiceInfo](../../models/components/serviceinfo.md) | :heavy_check_mark:                                               | Service information                                              |
| `severityText`                                                   | *string*                                                         | :heavy_minus_sign:                                               | Text representation of severity                                  |
| `spanId`                                                         | *string*                                                         | :heavy_minus_sign:                                               | W3C span ID (16 hex characters)                                  |
| `timeUnixNano`                                                   | *number*                                                         | :heavy_check_mark:                                               | Unix time in nanoseconds when event occurred                     |
| `traceId`                                                        | *string*                                                         | :heavy_minus_sign:                                               | W3C trace ID (32 hex characters)                                 |