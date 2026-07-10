# DeleteRemoteSessionIssuerRequest

## Example Usage

```typescript
import { DeleteRemoteSessionIssuerRequest } from "@gram/client/models/operations/deleteremotesessionissuer.js";

let value: DeleteRemoteSessionIssuerRequest = {
  id: "492a1c74-253d-4f53-b83d-b45a43eb992a",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_issuer id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |
| `gramProject`                 | *string*                      | :heavy_minus_sign:            | project header                |