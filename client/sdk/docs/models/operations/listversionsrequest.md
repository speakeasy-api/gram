# ListVersionsRequest

## Example Usage

```typescript
import { ListVersionsRequest } from "@gram/client/models/operations";

let value: ListVersionsRequest = {
  name: "<value>",
};
```

## Fields

| Field                   | Type                    | Required                | Description             |
| ----------------------- | ----------------------- | ----------------------- | ----------------------- |
| `name`                  | *string*                | :heavy_check_mark:      | The name of the package |
| `gramKey`               | *string*                | :heavy_minus_sign:      | API Key header          |
| `gramSession`           | *string*                | :heavy_minus_sign:      | Session header          |
| `gramProject`           | *string*                | :heavy_minus_sign:      | project header          |