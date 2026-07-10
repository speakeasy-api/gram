# DeleteOrganizationRemoteSessionIssuerRequest

## Example Usage

```typescript
import { DeleteOrganizationRemoteSessionIssuerRequest } from "@gram/client/models/operations/deleteorganizationremotesessionissuer.js";

let value: DeleteOrganizationRemoteSessionIssuerRequest = {
  id: "88fcef80-c496-49cd-bf65-687178154e97",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_issuer id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |