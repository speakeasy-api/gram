# DeleteChatRequest

## Example Usage

```typescript
import { DeleteChatRequest } from "@gram/client/models/operations/deletechat.js";

let value: DeleteChatRequest = {
  id: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                  |
| ------------- | -------- | ------------------ | ---------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the chat to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header               |
| `gramProject` | _string_ | :heavy_minus_sign: | project header               |
