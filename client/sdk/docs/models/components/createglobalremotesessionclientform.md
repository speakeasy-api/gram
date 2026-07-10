# CreateGlobalRemoteSessionClientForm

Form for creating a global remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.

## Example Usage

```typescript
import { CreateGlobalRemoteSessionClientForm } from "@gram/client/models/components/createglobalremotesessionclientform.js";

let value: CreateGlobalRemoteSessionClientForm = {
  clientId: "<id>",
  remoteSessionIssuerId: "fadf00c2-a7dc-4579-b945-a64a23654a0e",
};
```

## Fields

| Field                                                                                                                        | Type                                                                                                                         | Required                                                                                                                     | Description                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `audience`                                                                                                                   | *string*                                                                                                                     | :heavy_minus_sign:                                                                                                           | Optional upstream OAuth audience to send on the authorize redirect and token exchange.                                       |
| `clientId`                                                                                                                   | *string*                                                                                                                     | :heavy_check_mark:                                                                                                           | client_id supplied by the caller.                                                                                            |
| `clientSecret`                                                                                                               | *string*                                                                                                                     | :heavy_minus_sign:                                                                                                           | client_secret supplied by the caller. Gram encrypts before persisting.                                                       |
| `remoteSessionIssuerId`                                                                                                      | *string*                                                                                                                     | :heavy_check_mark:                                                                                                           | The owning global remote_session_issuer id.                                                                                  |
| `scope`                                                                                                                      | *string*[]                                                                                                                   | :heavy_minus_sign:                                                                                                           | Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported. |
| `tokenEndpointAuthMethod`                                                                                                    | [components.TokenEndpointAuthMethod](../../models/components/tokenendpointauthmethod.md)                                     | :heavy_minus_sign:                                                                                                           | How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.                         |