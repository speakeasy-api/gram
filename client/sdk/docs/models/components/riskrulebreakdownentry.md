# RiskRuleBreakdownEntry

## Example Usage

```typescript
import { RiskRuleBreakdownEntry } from "@gram/client/models/components/riskrulebreakdownentry.js";

let value: RiskRuleBreakdownEntry = {
  findings: 190526,
  ruleId: "<id>",
  source: "<value>",
};
```

## Fields

| Field                                                                                                           | Type                                                                                                            | Required                                                                                                        | Description                                                                                                     |
| --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `findings`                                                                                                      | *number*                                                                                                        | :heavy_check_mark:                                                                                              | Finding count for this rule within the window.                                                                  |
| `ruleId`                                                                                                        | *string*                                                                                                        | :heavy_check_mark:                                                                                              | Rule identifier (e.g. 'secret.aws-access-key'). Empty when the finding has no rule_id (treat as 'unspecified'). |
| `source`                                                                                                        | *string*                                                                                                        | :heavy_check_mark:                                                                                              | Source bucket the rule belongs to (gitleaks, presidio, etc.) for label/icon resolution on the dashboard.        |