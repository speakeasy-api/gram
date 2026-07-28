# ListCustomDetectionRulesResult

## Example Usage

```typescript
import { ListCustomDetectionRulesResult } from "@gram/client/models/components/listcustomdetectionrulesresult.js";

let value: ListCustomDetectionRulesResult = {
  rules: [
    {
      createdAt: new Date("2024-09-09T01:06:53.218Z"),
      description: "ew the recklessly venture",
      id: "231981d4-704b-4935-aae1-ab4c27cc2678",
      regex: "<value>",
      ruleId: "<id>",
      severity: "info",
      title: "<value>",
      updatedAt: new Date("2025-11-12T16:07:13.425Z"),
    },
  ],
};
```

## Fields

| Field   | Type                                                                                       | Required           | Description                         |
| ------- | ------------------------------------------------------------------------------------------ | ------------------ | ----------------------------------- |
| `rules` | [components.RiskCustomDetectionRule](../../models/components/riskcustomdetectionrule.md)[] | :heavy_check_mark: | The list of custom detection rules. |
