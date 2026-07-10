# RiskOverviewResult

## Example Usage

```typescript
import { RiskOverviewResult } from "@gram/client/models/components/riskoverviewresult.js";

let value: RiskOverviewResult = {
  activePolicies: 286510,
  findings: 269286,
  flaggedSessions: 31583,
  from: new Date("2024-01-17T22:37:21.883Z"),
  messagesScanned: 473276,
  timeSeriesFindings: [
    {
      bucketStart: new Date("2026-09-06T06:15:38.182Z"),
      category: "<value>",
      findings: 770221,
    },
  ],
  to: new Date("2026-01-16T02:07:19.497Z"),
  topCategories: [],
  topRules: [
    {
      findings: 959379,
      ruleId: "<id>",
      source: "<value>",
    },
  ],
  topUsers: [
    {
      email: "Drake.Tillman@hotmail.com",
      externalUserId: "<id>",
      findings: 848424,
    },
  ],
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `activePolicies`                                                                                       | *number*                                                                                               | :heavy_check_mark:                                                                                     | Enabled risk policies for the current project.                                                         |
| `findings`                                                                                             | *number*                                                                                               | :heavy_check_mark:                                                                                     | Policy findings in the window.                                                                         |
| `flaggedSessions`                                                                                      | *number*                                                                                               | :heavy_check_mark:                                                                                     | Chat sessions with at least one finding in the window.                                                 |
| `from`                                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)          | :heavy_check_mark:                                                                                     | Inclusive start of the overview window.                                                                |
| `messagesScanned`                                                                                      | *number*                                                                                               | :heavy_check_mark:                                                                                     | Messages analyzed by risk policies in the window.                                                      |
| `timeSeriesFindings`                                                                                   | [components.RiskOverviewTimeSeriesFinding](../../models/components/riskoverviewtimeseriesfinding.md)[] | :heavy_check_mark:                                                                                     | Time-series finding counts by category in the window.                                                  |
| `to`                                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)          | :heavy_check_mark:                                                                                     | Exclusive end of the overview window.                                                                  |
| `topCategories`                                                                                        | [components.RiskOverviewCategory](../../models/components/riskoverviewcategory.md)[]                   | :heavy_check_mark:                                                                                     | Top policy categories by finding count.                                                                |
| `topRules`                                                                                             | [components.RiskRuleBreakdownEntry](../../models/components/riskrulebreakdownentry.md)[]               | :heavy_check_mark:                                                                                     | Top rule_ids by finding count.                                                                         |
| `topUsers`                                                                                             | [components.RiskOverviewUser](../../models/components/riskoverviewuser.md)[]                           | :heavy_check_mark:                                                                                     | Top users by finding count.                                                                            |