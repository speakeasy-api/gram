# UploadChatAttachmentResult

## Example Usage

```typescript
import { UploadChatAttachmentResult } from "@gram/client/models/components";

let value: UploadChatAttachmentResult = {
  asset: {
    contentLength: 742982,
    contentType: "<value>",
    createdAt: new Date("2026-07-18T05:10:44.635Z"),
    id: "<id>",
    kind: "functions",
    sha256: "<value>",
    updatedAt: new Date("2024-11-04T23:49:28.974Z"),
  },
  url: "https://apprehensive-subsidy.com/",
};
```

## Fields

| Field                                                | Type                                                 | Required                                             | Description                                          |
| ---------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------- |
| `asset`                                              | [components.Asset](../../models/components/asset.md) | :heavy_check_mark:                                   | N/A                                                  |
| `url`                                                | *string*                                             | :heavy_check_mark:                                   | The URL to serve the chat attachment                 |