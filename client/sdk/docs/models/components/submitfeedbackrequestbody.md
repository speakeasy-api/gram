# SubmitFeedbackRequestBody

## Example Usage

```typescript
import { SubmitFeedbackRequestBody } from "@gram/client/models/components/submitfeedbackrequestbody.js";

let value: SubmitFeedbackRequestBody = {
  feedback: "success",
  id: "<id>",
};
```

## Fields

| Field      | Type                                                       | Required           | Description                       |
| ---------- | ---------------------------------------------------------- | ------------------ | --------------------------------- |
| `feedback` | [components.Feedback](../../models/components/feedback.md) | :heavy_check_mark: | User feedback: success or failure |
| `id`       | _string_                                                   | :heavy_check_mark: | The ID of the chat                |
