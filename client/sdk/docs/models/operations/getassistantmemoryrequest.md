# GetAssistantMemoryRequest

## Example Usage

```typescript
import { GetAssistantMemoryRequest } from "@gram/client/models/operations/getassistantmemory.js";

let value: GetAssistantMemoryRequest = {
  id: "e9c913dd-083d-4011-89bb-530d935f3586",
};
```

## Fields

| Field                    | Type                     | Required                 | Description              |
| ------------------------ | ------------------------ | ------------------------ | ------------------------ |
| `id`                     | *string*                 | :heavy_check_mark:       | The assistant memory ID. |
| `gramSession`            | *string*                 | :heavy_minus_sign:       | Session header           |
| `gramProject`            | *string*                 | :heavy_minus_sign:       | project header           |