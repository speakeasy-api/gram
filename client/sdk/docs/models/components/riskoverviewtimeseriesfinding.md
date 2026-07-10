# RiskOverviewTimeSeriesFinding

## Example Usage

```typescript
import { RiskOverviewTimeSeriesFinding } from "@gram/client/models/components/riskoverviewtimeseriesfinding.js";

let value: RiskOverviewTimeSeriesFinding = {
  bucketStart: new Date("2025-08-11T14:33:55.296Z"),
  category: "<value>",
  findings: 655334,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `bucketStart`                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Time bucket start.                                                                            |
| `category`                                                                                    | *string*                                                                                      | :heavy_check_mark:                                                                            | Policy category key.                                                                          |
| `findings`                                                                                    | *number*                                                                                      | :heavy_check_mark:                                                                            | Finding count for this category and time bucket.                                              |