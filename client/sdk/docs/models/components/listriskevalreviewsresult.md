# ListRiskEvalReviewsResult

## Example Usage

```typescript
import { ListRiskEvalReviewsResult } from "@gram/client/models/components/listriskevalreviewsresult.js";

let value: ListRiskEvalReviewsResult = {
  reviews: [
    {
      chatId: "06d5413b-2ac6-4cd7-a597-6b1e8c2c981c",
      createdAt: new Date("2026-04-26T03:31:50.751Z"),
      id: "6224c3a4-db0c-423a-9ed9-6d56578c33be",
      policyId: "1fa62d18-01a3-4d4c-8e4b-248a54722b3b",
      policyVersion: 336617,
      reviewedBy: "<value>",
      updatedAt: new Date("2025-11-18T00:01:04.987Z"),
      verdict: "missed",
    },
  ],
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `reviews`                                                                            | [components.RiskPolicyEvalReview](../../models/components/riskpolicyevalreview.md)[] | :heavy_check_mark:                                                                   | The active review set for the policy.                                                |