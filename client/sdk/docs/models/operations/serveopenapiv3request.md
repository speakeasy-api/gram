# ServeOpenAPIv3Request

## Example Usage

```typescript
import { ServeOpenAPIv3Request } from "@gram/client/models/operations";

let value: ServeOpenAPIv3Request = {
  id: "<id>",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `id`                         | *string*                     | :heavy_check_mark:           | The ID of the asset to serve |
| `gramKey`                    | *string*                     | :heavy_minus_sign:           | API Key header               |
| `gramProject`                | *string*                     | :heavy_minus_sign:           | project header               |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |