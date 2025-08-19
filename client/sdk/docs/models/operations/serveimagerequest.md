# ServeImageRequest

## Example Usage

```typescript
import { ServeImageRequest } from "@gram/client/models/operations";

let value: ServeImageRequest = {
  id: "<id>",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `id`                         | *string*                     | :heavy_check_mark:           | The ID of the asset to serve |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |
| `gramKey`                    | *string*                     | :heavy_minus_sign:           | API Key header               |