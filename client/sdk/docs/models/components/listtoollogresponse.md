# ListToolLogResponse

## Example Usage

```typescript
import { ListToolLogResponse } from "@gram/client/models/components";

let value: ListToolLogResponse = {
  enabled: false,
  logs: [
    {
      deploymentId: "6824c174-a496-42ca-b553-a5fd4c698fbb",
      durationMs: 7031.96,
      httpMethod: "<value>",
      httpRoute: "<value>",
      httpServerUrl: "https://joyous-roadway.net",
      organizationId: "84afd38d-ef12-4acc-be7b-e9b63e4eb5ec",
      spanId: "<id>",
      statusCode: 44086,
      toolId: "1b539e6e-859c-4f5c-b806-d1f6db86c008",
      toolType: "http",
      toolUrn: "<value>",
      traceId: "<id>",
      ts: new Date("2024-09-25T02:29:26.013Z"),
      userAgent: "<value>",
    },
  ],
  pagination: {},
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `enabled`                                                                      | *boolean*                                                                      | :heavy_check_mark:                                                             | Whether tool metrics are enabled for the organization                          |
| `logs`                                                                         | [components.HTTPToolLog](../../models/components/httptoollog.md)[]             | :heavy_check_mark:                                                             | N/A                                                                            |
| `pagination`                                                                   | [components.PaginationResponse](../../models/components/paginationresponse.md) | :heavy_check_mark:                                                             | Pagination metadata for list responses                                         |