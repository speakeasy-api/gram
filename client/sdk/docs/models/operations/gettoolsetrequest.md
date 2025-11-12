# GetToolsetRequest

## Example Usage

```typescript
import { GetToolsetRequest } from "@gram/client/models/operations";

let value: GetToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field                   | Type                    | Required                | Description             |
| ----------------------- | ----------------------- | ----------------------- | ----------------------- |
| `slug`                  | *string*                | :heavy_check_mark:      | The slug of the toolset |
| `gramSession`           | *string*                | :heavy_minus_sign:      | Session header          |
| `gramKey`               | *string*                | :heavy_minus_sign:      | API Key header          |
| `gramProject`           | *string*                | :heavy_minus_sign:      | project header          |