# GetProjectMetricsSummaryRequest

## Example Usage

```typescript
import { GetProjectMetricsSummaryRequest } from "@gram/client/models/operations";

let value: GetProjectMetricsSummaryRequest = {
  getProjectMetricsSummaryPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | API Key header                                                                                           |
| `gramSession`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Session header                                                                                           |
| `gramProject`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | project header                                                                                           |
| `getProjectMetricsSummaryPayload`                                                                        | [components.GetProjectMetricsSummaryPayload](../../models/components/getprojectmetricssummarypayload.md) | :heavy_check_mark:                                                                                       | N/A                                                                                                      |