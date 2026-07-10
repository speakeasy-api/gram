# SaveRiskEvalReviewRequestBody

## Example Usage

```typescript
import { SaveRiskEvalReviewRequestBody } from "@gram/client/models/components/saveriskevalreviewrequestbody.js";

let value: SaveRiskEvalReviewRequestBody = {
  chatId: "932aefb2-2768-4337-9a24-3c6b07632816",
  policyId: "9aa7870f-aae0-4f46-9c2b-57fc29918f9d",
  verdict: "false_positive",
};
```

## Fields

| Field      | Type                                                                                                               | Required           | Description                                           |
| ---------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ----------------------------------------------------- |
| `chatId`   | _string_                                                                                                           | :heavy_check_mark: | The chat session being judged.                        |
| `policyId` | _string_                                                                                                           | :heavy_check_mark: | The prompt-based policy the verdict belongs to.       |
| `verdict`  | [components.SaveRiskEvalReviewRequestBodyVerdict](../../models/components/saveriskevalreviewrequestbodyverdict.md) | :heavy_check_mark: | The reviewer's ground-truth verdict for this session. |
