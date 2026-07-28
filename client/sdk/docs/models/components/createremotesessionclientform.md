# CreateRemoteSessionClientForm

Form for creating a remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.

## Example Usage

```typescript
import { CreateRemoteSessionClientForm } from "@gram/client/models/components/createremotesessionclientform.js";

let value: CreateRemoteSessionClientForm = {
  clientId: "<id>",
  remoteSessionIssuerId: "3246f977-b8b8-4660-bdd3-4792104714f0",
};
```

## Fields

| Field                     | Type                                                                                                                                               | Required           | Description                                                                                                                                          |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `audience`                | _string_                                                                                                                                           | :heavy_minus_sign: | Optional upstream OAuth audience to send on the authorize redirect and token exchange.                                                               |
| `clientId`                | _string_                                                                                                                                           | :heavy_check_mark: | client_id supplied by the caller.                                                                                                                    |
| `clientSecret`            | _string_                                                                                                                                           | :heavy_minus_sign: | client_secret supplied by the caller. Gram encrypts before persisting.                                                                               |
| `remoteSessionIssuerId`   | _string_                                                                                                                                           | :heavy_check_mark: | The owning remote_session_issuer id.                                                                                                                 |
| `scope`                   | _string_[]                                                                                                                                         | :heavy_minus_sign: | Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.                         |
| `tokenEndpointAuthMethod` | [components.CreateRemoteSessionClientFormTokenEndpointAuthMethod](../../models/components/createremotesessionclientformtokenendpointauthmethod.md) | :heavy_minus_sign: | How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.                                                 |
| `userSessionIssuerIds`    | _string_[]                                                                                                                                         | :heavy_minus_sign: | The user_session_issuers to attach this client to via the join table. Omit or pass an empty array to create a standalone client with no attachments. |
