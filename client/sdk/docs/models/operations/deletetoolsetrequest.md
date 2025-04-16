# DeleteToolsetRequest

## Example Usage

```typescript
import { DeleteToolsetRequest } from "@gram/client/models/operations";

let value: DeleteToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field                   | Type                    | Required                | Description             |
| ----------------------- | ----------------------- | ----------------------- | ----------------------- |
| `slug`                  | *string*                | :heavy_check_mark:      | The slug of the toolset |
| `gramSession`           | *string*                | :heavy_minus_sign:      | Session header          |
| `gramProject`           | *string*                | :heavy_minus_sign:      | project header          |