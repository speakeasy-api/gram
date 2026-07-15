# DeleteRiskEvalReviewRequest

## Example Usage

```typescript
import { DeleteRiskEvalReviewRequest } from "@gram/client/models/operations/deleteriskevalreview.js";

let value: DeleteRiskEvalReviewRequest = {
  policyId: "82d016f6-eb4d-4009-9582-8f8ffb0f5a96",
  chatId: "22e9b52c-8124-4547-a382-33c9878ce839",
};
```

## Fields

| Field         | Type     | Required           | Description                              |
| ------------- | -------- | ------------------ | ---------------------------------------- |
| `policyId`    | _string_ | :heavy_check_mark: | The policy the verdict belongs to.       |
| `chatId`      | _string_ | :heavy_check_mark: | The chat session whose verdict to clear. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                           |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                           |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                           |
