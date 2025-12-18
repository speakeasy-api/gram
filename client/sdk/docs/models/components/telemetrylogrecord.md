# TelemetryLogRecord

OpenTelemetry log record

## Example Usage

```typescript
import { TelemetryLogRecord } from "@gram/client/models/components";

let value: TelemetryLogRecord = {
  attributes: "<value>",
  body: "<value>",
  gramProjectId: "b0b3b922-e157-405a-99f8-a1e7e6b0a753",
  gramUrn: "<value>",
  id: "0cbe895e-a42a-4109-83ee-fa9a8b6b6bfb",
  observedTimeUnixNano: 121676,
  resourceAttributes: "<value>",
  serviceName: "<value>",
  timeUnixNano: 841429,
};
```

## Fields

| Field                                            | Type                                             | Required                                         | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ |
| `attributes`                                     | *any*                                            | :heavy_check_mark:                               | Log attributes as JSON object                    |
| `body`                                           | *string*                                         | :heavy_check_mark:                               | The primary log message                          |
| `gramDeploymentId`                               | *string*                                         | :heavy_minus_sign:                               | Deployment ID                                    |
| `gramFunctionId`                                 | *string*                                         | :heavy_minus_sign:                               | Function ID                                      |
| `gramProjectId`                                  | *string*                                         | :heavy_check_mark:                               | Project ID                                       |
| `gramUrn`                                        | *string*                                         | :heavy_check_mark:                               | Gram URN                                         |
| `httpRequestMethod`                              | *string*                                         | :heavy_minus_sign:                               | HTTP method (null for non-HTTP logs)             |
| `httpResponseStatusCode`                         | *number*                                         | :heavy_minus_sign:                               | HTTP status code (null for non-HTTP logs)        |
| `httpRoute`                                      | *string*                                         | :heavy_minus_sign:                               | HTTP route (null for non-HTTP logs)              |
| `httpServerUrl`                                  | *string*                                         | :heavy_minus_sign:                               | HTTP server URL (null for non-HTTP logs)         |
| `id`                                             | *string*                                         | :heavy_check_mark:                               | Log record ID                                    |
| `observedTimeUnixNano`                           | *number*                                         | :heavy_check_mark:                               | Unix time in nanoseconds when event was observed |
| `resourceAttributes`                             | *any*                                            | :heavy_check_mark:                               | Resource attributes as JSON object               |
| `serviceName`                                    | *string*                                         | :heavy_check_mark:                               | Service name                                     |
| `serviceVersion`                                 | *string*                                         | :heavy_minus_sign:                               | Service version                                  |
| `severityText`                                   | *string*                                         | :heavy_minus_sign:                               | Text representation of severity                  |
| `spanId`                                         | *string*                                         | :heavy_minus_sign:                               | W3C span ID (16 hex characters)                  |
| `timeUnixNano`                                   | *number*                                         | :heavy_check_mark:                               | Unix time in nanoseconds when event occurred     |
| `traceId`                                        | *string*                                         | :heavy_minus_sign:                               | W3C trace ID (32 hex characters)                 |