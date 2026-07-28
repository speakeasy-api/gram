# GetRiskRuleBreakdownRequest

## Example Usage

```typescript
import { GetRiskRuleBreakdownRequest } from "@gram/client/models/operations/getriskrulebreakdown.js";

let value: GetRiskRuleBreakdownRequest = {
  category: "<value>",
};
```

## Fields

| Field         | Type                                                                                          | Required           | Description                                                                       |
| ------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------- |
| `category`    | _string_                                                                                      | :heavy_check_mark: | Required category key to break down by rule_id (e.g. secrets, pii).               |
| `from`        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Inclusive start of the window. Defaults to the same 7-day window as the overview. |
| `to`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Exclusive end of the window. Defaults to now.                                     |
| `gramKey`     | _string_                                                                                      | :heavy_minus_sign: | API Key header                                                                    |
| `gramSession` | _string_                                                                                      | :heavy_minus_sign: | Session header                                                                    |
| `gramProject` | _string_                                                                                      | :heavy_minus_sign: | project header                                                                    |
