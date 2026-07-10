# ChatOverview

## Example Usage

```typescript
import { ChatOverview } from "@gram/client/models/components/chatoverview.js";

let value: ChatOverview = {
  createdAt: new Date("2025-05-18T14:46:31.663Z"),
  id: "<id>",
  lastMessageTimestamp: new Date("2025-01-17T16:47:20.051Z"),
  numMessages: 2185,
  title: "<value>",
  updatedAt: new Date("2025-08-05T19:51:07.964Z"),
};
```

## Fields

| Field                  | Type                                                                                          | Required           | Description                                                                                                                                                     |
| ---------------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `accountEmail`         | _string_                                                                                      | :heavy_minus_sign: | Email of the AI account that produced the chat, resolved from the linked AI account. May differ from the employee's work email (e.g. a personal account).       |
| `accountType`          | _string_                                                                                      | :heavy_minus_sign: | Account type that produced the chat ('team', 'personal', or empty), resolved from the linked AI account.                                                        |
| `createdAt`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the chat was created.                                                                                                                                      |
| `externalUserId`       | _string_                                                                                      | :heavy_minus_sign: | The ID of the external user who created the chat                                                                                                                |
| `id`                   | _string_                                                                                      | :heavy_check_mark: | The ID of the chat                                                                                                                                              |
| `lastMessageTimestamp` | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the last message in the chat was created.                                                                                                                  |
| `numMessages`          | _number_                                                                                      | :heavy_check_mark: | The number of messages in the chat                                                                                                                              |
| `riskFindingsCount`    | _number_                                                                                      | :heavy_minus_sign: | Number of risk findings recorded against messages in this chat (project-scoped, found=true). Only populated by endpoints that join risk data; absent elsewhere. |
| `source`               | _string_                                                                                      | :heavy_minus_sign: | The source of the chat: Elements, Playground, ClaudeCode (inferred from messages)                                                                               |
| `title`                | _string_                                                                                      | :heavy_check_mark: | The title of the chat                                                                                                                                           |
| `totalCost`            | _number_                                                                                      | :heavy_minus_sign: | Total cost in USD for this chat                                                                                                                                 |
| `totalInputTokens`     | _number_                                                                                      | :heavy_minus_sign: | Total input tokens used in this chat                                                                                                                            |
| `totalOutputTokens`    | _number_                                                                                      | :heavy_minus_sign: | Total output tokens used in this chat                                                                                                                           |
| `totalTokens`          | _number_                                                                                      | :heavy_minus_sign: | Total tokens (input + output) used in this chat                                                                                                                 |
| `updatedAt`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the chat was last updated.                                                                                                                                 |
| `userId`               | _string_                                                                                      | :heavy_minus_sign: | The ID of the user who created the chat                                                                                                                         |
