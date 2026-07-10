# ListAssistantMemoriesResult

## Example Usage

```typescript
import { ListAssistantMemoriesResult } from "@gram/client/models/components/listassistantmemoriesresult.js";

let value: ListAssistantMemoriesResult = {
  memories: [
    {
      assistantId: "fc356b1d-a710-4d0e-a2d4-55ae9b886af2",
      content: "<value>",
      createdAt: new Date("2026-05-17T14:29:05.174Z"),
      id: "86477fe5-c6c6-4683-a812-256dcd791be1",
      lastAccess: new Date("2025-05-28T13:29:26.497Z"),
      tags: ["<value 1>", "<value 2>"],
      updatedAt: new Date("2025-04-26T13:08:47.242Z"),
      validAt: new Date("2025-08-13T21:47:54.136Z"),
    },
  ],
};
```

## Fields

| Field        | Type                                                                       | Required           | Description                                         |
| ------------ | -------------------------------------------------------------------------- | ------------------ | --------------------------------------------------- |
| `memories`   | [components.AssistantMemory](../../models/components/assistantmemory.md)[] | :heavy_check_mark: | Assistant memories matching the query.              |
| `nextCursor` | _string_                                                                   | :heavy_minus_sign: | The cursor to be used for the next page of results. |
