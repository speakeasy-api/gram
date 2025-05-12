# UploadImageRequest

## Example Usage

```typescript
import { UploadImageRequest } from "@gram/client/models/operations";

let value: UploadImageRequest = {
  contentLength: 7980,
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `contentLength`    | *number*           | :heavy_check_mark: | N/A                |
| `gramKey`          | *string*           | :heavy_minus_sign: | API Key header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |