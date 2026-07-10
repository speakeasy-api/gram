# ListRiskEvalReviewsRequest

## Example Usage

```typescript
import { ListRiskEvalReviewsRequest } from "@gram/client/models/operations/listriskevalreviews.js";

let value: ListRiskEvalReviewsRequest = {
  policyId: "dc6afaff-e044-45ab-8522-263b3346b751",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `policyId`                           | *string*                             | :heavy_check_mark:                   | The policy whose review set to list. |
| `gramKey`                            | *string*                             | :heavy_minus_sign:                   | API Key header                       |
| `gramSession`                        | *string*                             | :heavy_minus_sign:                   | Session header                       |
| `gramProject`                        | *string*                             | :heavy_minus_sign:                   | project header                       |