# ListToolLogResponse

## Example Usage

```typescript
import { ListToolLogResponse } from "@gram/client/models/components";

let value: ListToolLogResponse = {
  logs: [
    {
      deploymentId: "96824c17-4a49-462c-aa55-3a5fd4c698fb",
      durationMs: 8892.87,
      httpMethod: "<value>",
      httpRoute: "<value>",
      organizationId: "b87cb84a-fd38-4def-b12a-cce7be9b63e4",
      spanId: "<id>",
      statusCode: 693589,
      toolId: "5ecd01b5-39e6-4e85-99cf-5c806d1f6db8",
      toolType: "prompt",
      toolUrn: "<value>",
      traceId: "<id>",
      ts: new Date("2023-01-15T13:10:26.294Z"),
      userAgent: "<value>",
    },
  ],
  pagination: {},
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `logs`                                                                         | [components.HTTPToolLog](../../models/components/httptoollog.md)[]             | :heavy_check_mark:                                                             | N/A                                                                            |
| `pagination`                                                                   | [components.PaginationResponse](../../models/components/paginationresponse.md) | :heavy_check_mark:                                                             | Pagination metadata for list responses                                         |