# DeleteUserSessionIssuerRequest

## Example Usage

```typescript
import { DeleteUserSessionIssuerRequest } from "@gram/client/models/operations/deleteusersessionissuer.js";

let value: DeleteUserSessionIssuerRequest = {
  id: "69bb9cac-59d0-460e-ac10-03b0491fff07",
};
```

## Fields

| Field                       | Type                        | Required                    | Description                 |
| --------------------------- | --------------------------- | --------------------------- | --------------------------- |
| `id`                        | *string*                    | :heavy_check_mark:          | The user_session_issuer id. |
| `gramSession`               | *string*                    | :heavy_minus_sign:          | Session header              |
| `gramKey`                   | *string*                    | :heavy_minus_sign:          | API Key header              |
| `gramProject`               | *string*                    | :heavy_minus_sign:          | project header              |