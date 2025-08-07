# RemoveOAuthServerRequest

## Example Usage

```typescript
import { RemoveOAuthServerRequest } from "@gram/client/models/operations";

let value: RemoveOAuthServerRequest = {
  slug: "<value>",
};
```

## Fields

| Field                   | Type                    | Required                | Description             |
| ----------------------- | ----------------------- | ----------------------- | ----------------------- |
| `slug`                  | *string*                | :heavy_check_mark:      | The slug of the toolset |
| `gramSession`           | *string*                | :heavy_minus_sign:      | Session header          |
| `gramProject`           | *string*                | :heavy_minus_sign:      | project header          |