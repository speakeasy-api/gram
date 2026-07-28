# SubmitRiskBlockFeedbackRequest

## Example Usage

```typescript
import { SubmitRiskBlockFeedbackRequest } from "@gram/client/models/operations/submitriskblockfeedback.js";

let value: SubmitRiskBlockFeedbackRequest = {
  submitRiskBlockFeedbackRequestBody: {
    id: "2e59a02b-bd05-4ba6-b2bc-422a43fe13cf",
    sentiment: "down",
  },
};
```

## Fields

| Field                                | Type                                                                                                           | Required           | Description    |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                        | _string_                                                                                                       | :heavy_minus_sign: | Session header |
| `submitRiskBlockFeedbackRequestBody` | [components.SubmitRiskBlockFeedbackRequestBody](../../models/components/submitriskblockfeedbackrequestbody.md) | :heavy_check_mark: | N/A            |
