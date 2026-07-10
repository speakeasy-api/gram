# SetPinnedRequestBody

## Example Usage

```typescript
import { SetPinnedRequestBody } from "@gram/client/models/components/setpinnedrequestbody.js";

let value: SetPinnedRequestBody = {
  id: "<id>",
  pinned: false,
};
```

## Fields

| Field    | Type      | Required           | Description                             |
| -------- | --------- | ------------------ | --------------------------------------- |
| `id`     | _string_  | :heavy_check_mark: | The ID of the chat to pin or unpin      |
| `pinned` | _boolean_ | :heavy_check_mark: | True to pin the chat, false to unpin it |
