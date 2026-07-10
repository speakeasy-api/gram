# ListAttributeKeysRequest

## Example Usage

```typescript
import { ListAttributeKeysRequest } from "@gram/client/models/operations/listattributekeys.js";

let value: ListAttributeKeysRequest = {
  getProjectMetricsSummaryPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                             | Type                                                                                                     | Required           | Description    |
| --------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                         | _string_                                                                                                 | :heavy_minus_sign: | API Key header |
| `gramSession`                     | _string_                                                                                                 | :heavy_minus_sign: | Session header |
| `gramProject`                     | _string_                                                                                                 | :heavy_minus_sign: | project header |
| `getProjectMetricsSummaryPayload` | [components.GetProjectMetricsSummaryPayload](../../models/components/getprojectmetricssummarypayload.md) | :heavy_check_mark: | N/A            |
