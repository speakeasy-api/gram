# AssistantMemory

## Example Usage

```typescript
import { AssistantMemory } from "@gram/client/models/components/assistantmemory.js";

let value: AssistantMemory = {
  assistantId: "23278d27-9852-49b0-b6e0-59a491784172",
  content: "<value>",
  createdAt: new Date("2025-01-19T20:08:06.114Z"),
  id: "fa2d975e-5b21-4b33-8833-c671758202dc",
  lastAccess: new Date("2025-03-02T05:13:51.937Z"),
  tags: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  updatedAt: new Date("2026-04-10T12:25:17.156Z"),
  validAt: new Date("2024-10-06T21:02:57.597Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `assistantId`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The assistant ID owning the memory.                                                           |
| `content`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The memory content.                                                                           |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Creation timestamp.                                                                           |
| `deletedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Timestamp at which the memory was soft-deleted.                                               |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The assistant memory ID.                                                                      |
| `lastAccess`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Timestamp of the most recent access.                                                          |
| `supersededAt`                                                                                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Timestamp at which the memory was superseded by another memory.                               |
| `supersedesId`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the memory this one supersedes, if any.                                             |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_check_mark:                                                                            | Tags associated with the memory.                                                              |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Last update timestamp.                                                                        |
| `validAt`                                                                                     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Timestamp at which the memory becomes valid.                                                  |