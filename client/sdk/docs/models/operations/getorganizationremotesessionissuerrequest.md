# GetOrganizationRemoteSessionIssuerRequest

## Example Usage

```typescript
import { GetOrganizationRemoteSessionIssuerRequest } from "@gram/client/models/operations/getorganizationremotesessionissuer.js";

let value: GetOrganizationRemoteSessionIssuerRequest = {
  id: "fae39422-eb0f-4298-b4e7-308dc28866e1",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_issuer id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |