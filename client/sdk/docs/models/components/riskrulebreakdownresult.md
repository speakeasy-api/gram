# RiskRuleBreakdownResult

## Example Usage

```typescript
import { RiskRuleBreakdownResult } from "@gram/client/models/components/riskrulebreakdownresult.js";

let value: RiskRuleBreakdownResult = {
  category: "<value>",
  from: new Date("2025-04-05T19:18:20.686Z"),
  rules: [],
  to: new Date("2024-11-19T05:26:03.038Z"),
  total: 151900,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `category`                                                                                    | *string*                                                                                      | :heavy_check_mark:                                                                            | Category the breakdown is scoped to.                                                          |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Inclusive start of the window used.                                                           |
| `rules`                                                                                       | [components.RiskRuleBreakdownEntry](../../models/components/riskrulebreakdownentry.md)[]      | :heavy_check_mark:                                                                            | Rules in this category, ordered by finding count descending.                                  |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Exclusive end of the window used.                                                             |
| `total`                                                                                       | *number*                                                                                      | :heavy_check_mark:                                                                            | Total findings across all rules in this category and window.                                  |