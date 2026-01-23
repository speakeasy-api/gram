# CreateSignedChatAttachmentURLResult

## Example Usage

```typescript
import { CreateSignedChatAttachmentURLResult } from "@gram/client/models/components";

let value: CreateSignedChatAttachmentURLResult = {
  expiresAt: new Date("2026-09-18T08:10:37.819Z"),
  url: "https://reflecting-newsletter.name",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `expiresAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the signed URL expires                                                                   |
| `url`                                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The signed URL to access the chat attachment                                                  |