# RevokeAPIKeyRequest

## Example Usage

```typescript
import { RevokeAPIKeyRequest } from "@gram/client/models/operations";

let value: RevokeAPIKeyRequest = {
  id: "<id>",
};
```

## Fields

| Field                       | Type                        | Required                    | Description                 |
| --------------------------- | --------------------------- | --------------------------- | --------------------------- |
| `id`                        | *string*                    | :heavy_check_mark:          | The ID of the key to revoke |
| `gramSession`               | *string*                    | :heavy_minus_sign:          | Session header              |