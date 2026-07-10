# RiskUserBreakdownResult

## Example Usage

```typescript
import { RiskUserBreakdownResult } from "@gram/client/models/components/riskuserbreakdownresult.js";

let value: RiskUserBreakdownResult = {
  categories: [],
  externalUserId: "<id>",
  findings: 3406,
  from: new Date("2024-11-02T23:17:27.367Z"),
  rules: [
    {
      findings: 424826,
      ruleId: "<id>",
      source: "<value>",
    },
  ],
  to: new Date("2025-10-24T07:29:32.970Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `categories`                                                                                  | [components.RiskOverviewCategory](../../models/components/riskoverviewcategory.md)[]          | :heavy_check_mark:                                                                            | Category breakdown for this user, ordered by finding count descending.                        |
| `externalUserId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | External user the breakdown is scoped to.                                                     |
| `findings`                                                                                    | *number*                                                                                      | :heavy_check_mark:                                                                            | Total findings for this user in the window.                                                   |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Inclusive start of the window used.                                                           |
| `rules`                                                                                       | [components.RiskRuleBreakdownEntry](../../models/components/riskrulebreakdownentry.md)[]      | :heavy_check_mark:                                                                            | Rule_id breakdown for this user, ordered by finding count descending.                         |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Exclusive end of the window used.                                                             |