# SubmitRiskBlockFeedbackRequestBody

## Example Usage

```typescript
import { SubmitRiskBlockFeedbackRequestBody } from "@gram/client/models/components/submitriskblockfeedbackrequestbody.js";

let value: SubmitRiskBlockFeedbackRequestBody = {
  id: "40a451c7-08f4-4c74-a2af-b27b21914e1a",
  sentiment: "down",
};
```

## Fields

| Field       | Type                                                         | Required           | Description                                   |
| ----------- | ------------------------------------------------------------ | ------------------ | --------------------------------------------- |
| `id`        | _string_                                                     | :heavy_check_mark: | The block ID (the underlying risk result ID). |
| `sentiment` | [components.Sentiment](../../models/components/sentiment.md) | :heavy_check_mark: | Feedback sentiment.                           |
