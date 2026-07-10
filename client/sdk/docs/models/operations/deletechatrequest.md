# DeleteChatRequest

## Example Usage

```typescript
import { DeleteChatRequest } from "@gram/client/models/operations/deletechat.js";

let value: DeleteChatRequest = {
  id: "<id>",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `id`                         | *string*                     | :heavy_check_mark:           | The ID of the chat to delete |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |
| `gramProject`                | *string*                     | :heavy_minus_sign:           | project header               |