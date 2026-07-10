# GetUserSessionIssuerRequest

## Example Usage

```typescript
import { GetUserSessionIssuerRequest } from "@gram/client/models/operations/getusersessionissuer.js";

let value: GetUserSessionIssuerRequest = {};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_minus_sign:            | The user_session_issuer id.   |
| `slug`                        | *string*                      | :heavy_minus_sign:            | The user_session_issuer slug. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |
| `gramProject`                 | *string*                      | :heavy_minus_sign:            | project header                |