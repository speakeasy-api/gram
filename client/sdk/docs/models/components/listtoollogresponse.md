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
      organizationId: "87cb84af-d38d-4ef1-a2ac-ce7be9b63e4e",
      spanId: "<id>",
      statusCode: 369282,
      toolId: "ecd01b53-9e6e-4859-bcf5-c806d1f6db86",
      toolType: "http",
      toolUrn: "<value>",
      traceId: "<id>",
      ts: new Date("2023-02-23T09:03:30.725Z"),
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