# CreateSignedChatAttachmentURLForm2

## Example Usage

```typescript
import { CreateSignedChatAttachmentURLForm2 } from "@gram/client/models/components/createsignedchatattachmenturlform2.js";

let value: CreateSignedChatAttachmentURLForm2 = {
  id: "<id>",
  projectId: "<id>",
};
```

## Fields

| Field        | Type     | Required           | Description                                       |
| ------------ | -------- | ------------------ | ------------------------------------------------- |
| `id`         | _string_ | :heavy_check_mark: | The ID of the chat attachment                     |
| `projectId`  | _string_ | :heavy_check_mark: | The project ID that the attachment belongs to     |
| `ttlSeconds` | _number_ | :heavy_minus_sign: | Time-to-live in seconds (default: 600, max: 3600) |
