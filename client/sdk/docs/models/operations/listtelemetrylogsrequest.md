# ListTelemetryLogsRequest

## Example Usage

```typescript
import { ListTelemetryLogsRequest } from "@gram/client/models/operations";

let value: ListTelemetryLogsRequest = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `timeStart`                                                            | *number*                                                               | :heavy_minus_sign:                                                     | Start time in Unix nanoseconds                                         |
| `timeEnd`                                                              | *number*                                                               | :heavy_minus_sign:                                                     | End time in Unix nanoseconds                                           |
| `gramUrn`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | Gram URN filter                                                        |
| `traceId`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | Trace ID filter (32 hex characters)                                    |
| `deploymentId`                                                         | *string*                                                               | :heavy_minus_sign:                                                     | Deployment ID filter                                                   |
| `functionId`                                                           | *string*                                                               | :heavy_minus_sign:                                                     | Function ID filter                                                     |
| `severityText`                                                         | [operations.SeverityText](../../models/operations/severitytext.md)     | :heavy_minus_sign:                                                     | Severity level filter                                                  |
| `httpStatusCode`                                                       | *number*                                                               | :heavy_minus_sign:                                                     | HTTP status code filter                                                |
| `httpRoute`                                                            | *string*                                                               | :heavy_minus_sign:                                                     | HTTP route filter                                                      |
| `httpMethod`                                                           | [operations.HttpMethod](../../models/operations/httpmethod.md)         | :heavy_minus_sign:                                                     | HTTP method filter                                                     |
| `serviceName`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Service name filter                                                    |
| `cursor`                                                               | *string*                                                               | :heavy_minus_sign:                                                     | Cursor for pagination                                                  |
| `limit`                                                                | *number*                                                               | :heavy_minus_sign:                                                     | Number of items to return (1-1000)                                     |
| `sort`                                                                 | [operations.QueryParamSort](../../models/operations/queryparamsort.md) | :heavy_minus_sign:                                                     | Sort order                                                             |
| `gramKey`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | API Key header                                                         |
| `gramSession`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Session header                                                         |
| `gramProject`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | project header                                                         |