# UploadFunctionsRequest

## Example Usage

```typescript
import { UploadFunctionsRequest } from "@gram/client/models/operations";

// No examples available for this model
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `contentLength`              | *number*                     | :heavy_check_mark:           | N/A                          |
| `gramKey`                    | *string*                     | :heavy_minus_sign:           | API Key header               |
| `gramProject`                | *string*                     | :heavy_minus_sign:           | project header               |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |
| `requestBody`                | *ReadableStream<Uint8Array>* | :heavy_check_mark:           | N/A                          |