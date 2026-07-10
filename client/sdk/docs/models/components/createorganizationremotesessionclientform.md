# CreateOrganizationRemoteSessionClientForm

Form for an org admin to register a standalone remote_session_client under an existing issuer, with no user_session_issuer attachments.

## Example Usage

```typescript
import { CreateOrganizationRemoteSessionClientForm } from "@gram/client/models/components/createorganizationremotesessionclientform.js";

let value: CreateOrganizationRemoteSessionClientForm = {
  clientId: "<id>",
  remoteSessionIssuerId: "b7c75f4d-0dbb-4e1e-bf36-eb6b3acf4cda",
};
```

## Fields

| Field                     | Type                                                                                                                                                                       | Required           | Description                                                                                                                                                                                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `audience`                | _string_                                                                                                                                                                   | :heavy_minus_sign: | Optional upstream OAuth audience to send on the authorize redirect and token exchange.                                                                                                                                                                                   |
| `clientId`                | _string_                                                                                                                                                                   | :heavy_check_mark: | client_id supplied by the caller, e.g. from Dynamic Client Registration.                                                                                                                                                                                                 |
| `clientSecret`            | _string_                                                                                                                                                                   | :heavy_minus_sign: | Optional client_secret supplied by the caller. Gram encrypts before persisting; the plaintext is never returned.                                                                                                                                                         |
| `projectId`               | _string_                                                                                                                                                                   | :heavy_minus_sign: | Owning project id for the new client; the project must belong to the caller's organization. Omit to inherit a project-specific issuer's project, or to create an organization-level client (no project, attachable by every project) under an organization-level issuer. |
| `remoteSessionIssuerId`   | _string_                                                                                                                                                                   | :heavy_check_mark: | The owning remote_session_issuer id; must belong to the caller's organization.                                                                                                                                                                                           |
| `scope`                   | _string_[]                                                                                                                                                                 | :heavy_minus_sign: | Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.                                                                                                                                             |
| `tokenEndpointAuthMethod` | [components.CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod](../../models/components/createorganizationremotesessionclientformtokenendpointauthmethod.md) | :heavy_minus_sign: | How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.                                                                                                                                                                     |
