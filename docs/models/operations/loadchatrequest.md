# LoadChatRequest

## Example Usage

```typescript
import { LoadChatRequest } from "@gram/client/models/operations";

let value: LoadChatRequest = {
  id: "<id>",
};
```

## Fields

| Field                      | Type                       | Required                   | Description                |
| -------------------------- | -------------------------- | -------------------------- | -------------------------- |
| `id`                       | *string*                   | :heavy_check_mark:         | The ID of the chat         |
| `gramSession`              | *string*                   | :heavy_minus_sign:         | Session header             |
| `gramProject`              | *string*                   | :heavy_minus_sign:         | project header             |
| `gramChatSession`          | *string*                   | :heavy_minus_sign:         | Chat Sessions token header |