# ServeChatAttachmentRequest

## Example Usage

```typescript
import { ServeChatAttachmentRequest } from "@gram/client/models/operations";

let value: ServeChatAttachmentRequest = {
  id: "<id>",
  projectId: "<id>",
};
```

## Fields

| Field                                         | Type                                          | Required                                      | Description                                   |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `id`                                          | *string*                                      | :heavy_check_mark:                            | The ID of the attachment to serve             |
| `projectId`                                   | *string*                                      | :heavy_check_mark:                            | The project ID that the attachment belongs to |
| `gramKey`                                     | *string*                                      | :heavy_minus_sign:                            | API Key header                                |
| `gramSession`                                 | *string*                                      | :heavy_minus_sign:                            | Session header                                |
| `gramChatSession`                             | *string*                                      | :heavy_minus_sign:                            | Chat Sessions token header                    |