# HTTPToolLog

HTTP tool request and response log entry

## Example Usage

```typescript
import { HTTPToolLog } from "@gram/client/models/components";

let value: HTTPToolLog = {
  deploymentId: "872017e0-831c-48b1-b0cc-4435bccf7f84",
  durationMs: 5054.62,
  httpMethod: "<value>",
  httpRoute: "<value>",
  httpServerUrl: "https://oblong-contrail.biz/",
  organizationId: "092c52f8-c1be-4a0c-bfcb-d19b476d8873",
  spanId: "<id>",
  statusCode: 28790,
  toolId: "840e0040-7866-44ce-97e8-9950e4063310",
  toolType: "http",
  toolUrn: "<value>",
  traceId: "<id>",
  ts: new Date("2024-08-12T03:36:10.953Z"),
  userAgent: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Deployment UUID                                                                               |
| `durationMs`                                                                                  | *number*                                                                                      | :heavy_check_mark:                                                                            | Duration in milliseconds                                                                      |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP method                                                                                   |
| `httpRoute`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP route                                                                                    |
| `httpServerUrl`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP Server URL                                                                               |
| `id`                                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | Id of the request                                                                             |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | Organization UUID                                                                             |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | Project UUID                                                                                  |
| `requestBodyBytes`                                                                            | *number*                                                                                      | :heavy_minus_sign:                                                                            | Request body size in bytes                                                                    |
| `requestHeaders`                                                                              | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | Request headers                                                                               |
| `responseBodyBytes`                                                                           | *number*                                                                                      | :heavy_minus_sign:                                                                            | Response body size in bytes                                                                   |
| `responseHeaders`                                                                             | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | Response headers                                                                              |
| `spanId`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Span ID for correlation                                                                       |
| `statusCode`                                                                                  | *number*                                                                                      | :heavy_check_mark:                                                                            | HTTP status code                                                                              |
| `toolId`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Tool UUID                                                                                     |
| `toolType`                                                                                    | [components.ToolType](../../models/components/tooltype.md)                                    | :heavy_check_mark:                                                                            | Type of tool being logged                                                                     |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Tool URN                                                                                      |
| `traceId`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Trace ID for correlation                                                                      |
| `ts`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Timestamp of the request                                                                      |
| `userAgent`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | User agent                                                                                    |