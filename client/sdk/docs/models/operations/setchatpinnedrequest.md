# SetChatPinnedRequest

## Example Usage

```typescript
import { SetChatPinnedRequest } from "@gram/client/models/operations/setchatpinned.js";

let value: SetChatPinnedRequest = {
  setPinnedRequestBody: {
    id: "<id>",
    pinned: true,
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `gramSession`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | Session header                                                                     |
| `gramProject`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | project header                                                                     |
| `setPinnedRequestBody`                                                             | [components.SetPinnedRequestBody](../../models/components/setpinnedrequestbody.md) | :heavy_check_mark:                                                                 | N/A                                                                                |