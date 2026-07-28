# RiskChatSummary

## Example Usage

```typescript
import { RiskChatSummary } from "@gram/client/models/components/riskchatsummary.js";

let value: RiskChatSummary = {
  chatId: "792f7bb3-e8c8-41c0-986a-94654f09ee86",
  findingsCount: 782021,
  latestDetected: new Date("2024-09-05T06:02:37.079Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------ |
| `chatId`         | _string_                                                                                      | :heavy_check_mark: | The chat session ID.                       |
| `chatTitle`      | _string_                                                                                      | :heavy_minus_sign: | Title of the chat session.                 |
| `findingsCount`  | _number_                                                                                      | :heavy_check_mark: | Number of findings in this chat.           |
| `latestDetected` | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the most recent finding was detected. |
| `userId`         | _string_                                                                                      | :heavy_minus_sign: | The user who owns the chat session.        |
