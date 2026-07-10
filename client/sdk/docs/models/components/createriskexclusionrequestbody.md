# CreateRiskExclusionRequestBody

## Example Usage

```typescript
import { CreateRiskExclusionRequestBody } from "@gram/client/models/components/createriskexclusionrequestbody.js";

let value: CreateRiskExclusionRequestBody = {
  matchType: "regex",
  matchValue: "<value>",
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `enabled`                                                                          | *boolean*                                                                          | :heavy_minus_sign:                                                                 | Whether the exclusion is active.                                                   |
| `matchType`                                                                        | [components.MatchType](../../models/components/matchtype.md)                       | :heavy_check_mark:                                                                 | How match_value is interpreted.                                                    |
| `matchValue`                                                                       | *string*                                                                           | :heavy_check_mark:                                                                 | The value matched against findings, interpreted per match_type.                    |
| `riskPolicyId`                                                                     | *string*                                                                           | :heavy_minus_sign:                                                                 | Bind the exclusion to a single policy. Omit for a global (project-wide) exclusion. |
| `ruleIdFilter`                                                                     | *string*                                                                           | :heavy_minus_sign:                                                                 | Optional: only apply within this rule_id. Empty means any.                         |
| `sourceFilter`                                                                     | *string*                                                                           | :heavy_minus_sign:                                                                 | Optional: only apply within this source. Empty means any.                          |