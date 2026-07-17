# SubmitFeedbackRequest

## Example Usage

```typescript
import { SubmitFeedbackRequest } from "@gram/client/models/operations/submitfeedback.js";

let value: SubmitFeedbackRequest = {
  submitFeedbackRequestBody: {
    feedback: "success",
    id: "<id>",
  },
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description                |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------------------- |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header             |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header             |
| `gramChatSession`           | _string_                                                                                     | :heavy_minus_sign: | Chat Sessions token header |
| `submitFeedbackRequestBody` | [components.SubmitFeedbackRequestBody](../../models/components/submitfeedbackrequestbody.md) | :heavy_check_mark: | N/A                        |
