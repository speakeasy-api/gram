# ServeChatAttachmentRequest

## Example Usage

```typescript
import { ServeChatAttachmentRequest } from "@gram/client/models/operations/servechatattachment.js";

let value: ServeChatAttachmentRequest = {
  id: "<id>",
  projectId: "<id>",
};
```

## Fields

| Field             | Type     | Required           | Description                                   |
| ----------------- | -------- | ------------------ | --------------------------------------------- |
| `id`              | _string_ | :heavy_check_mark: | The ID of the attachment to serve             |
| `projectId`       | _string_ | :heavy_check_mark: | The project ID that the attachment belongs to |
| `gramKey`         | _string_ | :heavy_minus_sign: | API Key header                                |
| `gramSession`     | _string_ | :heavy_minus_sign: | Session header                                |
| `gramChatSession` | _string_ | :heavy_minus_sign: | Chat Sessions token header                    |
