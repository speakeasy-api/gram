# ListAssistantMemoriesResponse

## Example Usage

```typescript
import { ListAssistantMemoriesResponse } from "@gram/client/models/operations/listassistantmemories.js";

let value: ListAssistantMemoriesResponse = {
  result: {
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
  },
};
```

## Fields

| Field    | Type                                                                                             | Required           | Description |
| -------- | ------------------------------------------------------------------------------------------------ | ------------------ | ----------- |
| `result` | [components.ListAssistantMemoriesResult](../../models/components/listassistantmemoriesresult.md) | :heavy_check_mark: | N/A         |
