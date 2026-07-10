# RiskPolicyEvalReview

## Example Usage

```typescript
import { RiskPolicyEvalReview } from "@gram/client/models/components/riskpolicyevalreview.js";

let value: RiskPolicyEvalReview = {
  chatId: "7f26e6bd-d138-4108-a344-285a9d097c2b",
  createdAt: new Date("2024-10-12T19:07:08.473Z"),
  id: "7069ad99-7c7c-4f7b-b023-0ae529872cf3",
  policyId: "1e07f8dc-22b6-46bd-8774-b669a83c6a5f",
  policyVersion: 176774,
  reviewedBy: "<value>",
  updatedAt: new Date("2026-02-24T18:12:50.962Z"),
  verdict: "false_positive",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `chatId`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The chat session being judged.                                                                |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the verdict was first recorded.                                                          |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The review ID.                                                                                |
| `policyId`                                                                                    | *string*                                                                                      | :heavy_check_mark:                                                                            | The prompt-based policy the verdict belongs to.                                               |
| `policyVersion`                                                                               | *number*                                                                                      | :heavy_check_mark:                                                                            | The policy version in effect when the verdict was recorded (provenance).                      |
| `reviewedBy`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | User id of the reviewer who recorded the verdict.                                             |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the verdict was last updated.                                                            |
| `verdict`                                                                                     | [components.Verdict](../../models/components/verdict.md)                                      | :heavy_check_mark:                                                                            | The reviewer's ground-truth verdict.                                                          |