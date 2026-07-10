# SaveRiskEvalReviewRequest

## Example Usage

```typescript
import { SaveRiskEvalReviewRequest } from "@gram/client/models/operations/saveriskevalreview.js";

let value: SaveRiskEvalReviewRequest = {
  saveRiskEvalReviewRequestBody: {
    chatId: "a62c6d27-eb08-4ab3-bcd2-d8fd6cd8c578",
    policyId: "e58afe19-40d1-42d1-8710-fcb84a39c0e7",
    verdict: "missed",
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                       | _string_                                                                                             | :heavy_minus_sign: | API Key header |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header |
| `gramProject`                   | _string_                                                                                             | :heavy_minus_sign: | project header |
| `saveRiskEvalReviewRequestBody` | [components.SaveRiskEvalReviewRequestBody](../../models/components/saveriskevalreviewrequestbody.md) | :heavy_check_mark: | N/A            |
