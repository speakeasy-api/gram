# GetOrganizationRemoteSessionIssuerDeletePreflightRequest

## Example Usage

```typescript
import { GetOrganizationRemoteSessionIssuerDeletePreflightRequest } from "@gram/client/models/operations/getorganizationremotesessionissuerdeletepreflight.js";

let value: GetOrganizationRemoteSessionIssuerDeletePreflightRequest = {
  id: "2c193999-4acc-461a-8dc4-5a7cb5fbe315",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_issuer id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |