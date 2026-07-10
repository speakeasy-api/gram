# UpdateRiskExclusionRequestBody

## Example Usage

```typescript
import { UpdateRiskExclusionRequestBody } from "@gram/client/models/components/updateriskexclusionrequestbody.js";

let value: UpdateRiskExclusionRequestBody = {
  id: "8a15e830-67a1-4df1-af47-89de6e4dfcf6",
  matchType: "exact",
  matchValue: "<value>",
};
```

## Fields

| Field                                                                                                                    | Type                                                                                                                     | Required                                                                                                                 | Description                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `enabled`                                                                                                                | *boolean*                                                                                                                | :heavy_minus_sign:                                                                                                       | Whether the exclusion is active. Omit to leave unchanged.                                                                |
| `id`                                                                                                                     | *string*                                                                                                                 | :heavy_check_mark:                                                                                                       | The exclusion ID.                                                                                                        |
| `matchType`                                                                                                              | [components.UpdateRiskExclusionRequestBodyMatchType](../../models/components/updateriskexclusionrequestbodymatchtype.md) | :heavy_check_mark:                                                                                                       | How match_value is interpreted.                                                                                          |
| `matchValue`                                                                                                             | *string*                                                                                                                 | :heavy_check_mark:                                                                                                       | The value matched against findings, interpreted per match_type.                                                          |
| `riskPolicyId`                                                                                                           | *string*                                                                                                                 | :heavy_minus_sign:                                                                                                       | Bind the exclusion to a single policy. Omit for a global (project-wide) exclusion.                                       |
| `ruleIdFilter`                                                                                                           | *string*                                                                                                                 | :heavy_minus_sign:                                                                                                       | Optional: only apply within this rule_id. Empty means any.                                                               |
| `sourceFilter`                                                                                                           | *string*                                                                                                                 | :heavy_minus_sign:                                                                                                       | Optional: only apply within this source. Empty means any.                                                                |