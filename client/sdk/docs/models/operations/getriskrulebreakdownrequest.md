# GetRiskRuleBreakdownRequest

## Example Usage

```typescript
import { GetRiskRuleBreakdownRequest } from "@gram/client/models/operations/getriskrulebreakdown.js";

let value: GetRiskRuleBreakdownRequest = {
  category: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `category`                                                                                    | *string*                                                                                      | :heavy_check_mark:                                                                            | Required category key to break down by rule_id (e.g. secrets, pii).                           |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Inclusive start of the window. Defaults to the same 7-day window as the overview.             |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Exclusive end of the window. Defaults to now.                                                 |
| `gramKey`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | API Key header                                                                                |
| `gramSession`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Session header                                                                                |
| `gramProject`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | project header                                                                                |