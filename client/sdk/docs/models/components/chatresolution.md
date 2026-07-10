# ChatResolution

Resolution information for a chat

## Example Usage

```typescript
import { ChatResolution } from "@gram/client/models/components";

let value: ChatResolution = {
  createdAt: new Date("2026-09-13T07:57:01.152Z"),
  id: "80e24b0d-9959-4fa6-b9ae-039c286697a0",
  messageIds: ["abc-123", "def-456"],
  resolution: "<value>",
  resolutionNotes: "<value>",
  score: 841888,
  userGoal: "<value>",
};
```

## Fields

| Field             | Type                                                                                          | Required           | Description                                 | Example                              |
| ----------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------- | ------------------------------------ |
| `createdAt`       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When resolution was created                 |                                      |
| `id`              | _string_                                                                                      | :heavy_check_mark: | Resolution ID                               |                                      |
| `messageIds`      | _string_[]                                                                                    | :heavy_check_mark: | Message IDs associated with this resolution | [<br/>"abc-123",<br/>"def-456"<br/>] |
| `resolution`      | _string_                                                                                      | :heavy_check_mark: | Resolution status                           |                                      |
| `resolutionNotes` | _string_                                                                                      | :heavy_check_mark: | Notes about the resolution                  |                                      |
| `score`           | _number_                                                                                      | :heavy_check_mark: | Score 0-100                                 |                                      |
| `userGoal`        | _string_                                                                                      | :heavy_check_mark: | User's intended goal                        |                                      |
