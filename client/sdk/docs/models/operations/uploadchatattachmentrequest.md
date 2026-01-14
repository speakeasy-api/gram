# UploadChatAttachmentRequest

## Example Usage

```typescript
import { UploadChatAttachmentRequest } from "@gram/client/models/operations";

let value: UploadChatAttachmentRequest = {
  contentLength: 506371,
};
```

## Fields

| Field                      | Type                       | Required                   | Description                |
| -------------------------- | -------------------------- | -------------------------- | -------------------------- |
| `contentLength`            | *number*                   | :heavy_check_mark:         | N/A                        |
| `gramKey`                  | *string*                   | :heavy_minus_sign:         | API Key header             |
| `gramProject`              | *string*                   | :heavy_minus_sign:         | project header             |
| `gramSession`              | *string*                   | :heavy_minus_sign:         | Session header             |
| `gramChatSession`          | *string*                   | :heavy_minus_sign:         | Chat Sessions token header |