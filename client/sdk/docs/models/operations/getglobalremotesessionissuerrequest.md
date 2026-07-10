# GetGlobalRemoteSessionIssuerRequest

## Example Usage

```typescript
import { GetGlobalRemoteSessionIssuerRequest } from "@gram/client/models/operations/getglobalremotesessionissuer.js";

let value: GetGlobalRemoteSessionIssuerRequest = {
  id: "5e0f4df7-d127-4010-8810-74eaddd15fa4",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_issuer id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |