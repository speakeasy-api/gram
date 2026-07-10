# RiskCategoryDefinition

One canonical risk category and how findings are classified into it.

## Example Usage

```typescript
import { RiskCategoryDefinition } from "@gram/client/models/components/riskcategorydefinition.js";

let value: RiskCategoryDefinition = {
  description: "deck proceed sizzling extremely an overfeed pfft to",
  icon: "<value>",
  key: "<key>",
  label: "<value>",
  ruleIdPrefix: "<value>",
  ruleIds: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  source: "<value>",
};
```

## Fields

| Field                                                                                                                             | Type                                                                                                                              | Required                                                                                                                          | Description                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `description`                                                                                                                     | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | Plain-English description of what this category covers.                                                                           |
| `icon`                                                                                                                            | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | Lucide icon name suggested for this category.                                                                                     |
| `key`                                                                                                                             | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | Canonical category key (e.g. 'secrets', 'pii', 'shadow_mcp').                                                                     |
| `label`                                                                                                                           | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | Human-readable category label for UI rendering.                                                                                   |
| `ruleIdPrefix`                                                                                                                    | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | When non-empty, findings whose rule_id starts with this prefix belong to this category. The catch-all for a family (e.g. 'pii.'). |
| `ruleIds`                                                                                                                         | *string*[]                                                                                                                        | :heavy_check_mark:                                                                                                                | When non-empty, findings whose rule_id is in this exact list belong to this category. Checked before rule_id_prefix.              |
| `source`                                                                                                                          | *string*                                                                                                                          | :heavy_check_mark:                                                                                                                | When non-empty, findings whose source equals this value belong to this category.                                                  |