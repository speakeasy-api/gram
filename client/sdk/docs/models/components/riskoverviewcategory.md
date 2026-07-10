# RiskOverviewCategory

## Example Usage

```typescript
import { RiskOverviewCategory } from "@gram/client/models/components/riskoverviewcategory.js";

let value: RiskOverviewCategory = {
  category: "<value>",
  findings: 94239,
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `category`                       | *string*                         | :heavy_check_mark:               | Policy category key.             |
| `findings`                       | *number*                         | :heavy_check_mark:               | Finding count for this category. |