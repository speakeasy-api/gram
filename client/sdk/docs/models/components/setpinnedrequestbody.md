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

| Field                                   | Type                                    | Required                                | Description                             |
| --------------------------------------- | --------------------------------------- | --------------------------------------- | --------------------------------------- |
| `id`                                    | *string*                                | :heavy_check_mark:                      | The ID of the chat to pin or unpin      |
| `pinned`                                | *boolean*                               | :heavy_check_mark:                      | True to pin the chat, false to unpin it |