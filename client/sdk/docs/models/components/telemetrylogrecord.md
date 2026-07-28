# TelemetryLogRecord

OpenTelemetry log record

## Example Usage

```typescript
import { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";

let value: TelemetryLogRecord = {
  attributes: "<value>",
  body: "<value>",
  id: "b0b3b922-e157-405a-99f8-a1e7e6b0a753",
  observedTimeUnixNano: "<value>",
  resourceAttributes: "<value>",
  service: {
    name: "<value>",
  },
  timeUnixNano: "<value>",
};
```

## Fields

| Field                  | Type                                                             | Required           | Description                                                                      |
| ---------------------- | ---------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------- |
| `attributes`           | _any_                                                            | :heavy_check_mark: | Log attributes as JSON object                                                    |
| `body`                 | _string_                                                         | :heavy_check_mark: | The primary log message                                                          |
| `id`                   | _string_                                                         | :heavy_check_mark: | Log record ID                                                                    |
| `observedTimeUnixNano` | _string_                                                         | :heavy_check_mark: | Unix time in nanoseconds when event was observed (string for JS int64 precision) |
| `resourceAttributes`   | _any_                                                            | :heavy_check_mark: | Resource attributes as JSON object                                               |
| `service`              | [components.ServiceInfo](../../models/components/serviceinfo.md) | :heavy_check_mark: | Service information                                                              |
| `severityText`         | _string_                                                         | :heavy_minus_sign: | Text representation of severity                                                  |
| `spanId`               | _string_                                                         | :heavy_minus_sign: | W3C span ID (16 hex characters)                                                  |
| `timeUnixNano`         | _string_                                                         | :heavy_check_mark: | Unix time in nanoseconds when event occurred (string for JS int64 precision)     |
| `traceId`              | _string_                                                         | :heavy_minus_sign: | W3C trace ID (32 hex characters)                                                 |
