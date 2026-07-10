# UpdateRemoteSessionClientForm

Form for updating a remote_session_client. All non-id fields are optional patches.

## Example Usage

```typescript
import { UpdateRemoteSessionClientForm } from "@gram/client/models/components/updateremotesessionclientform.js";

let value: UpdateRemoteSessionClientForm = {
  id: "3dcf3c0d-59c7-4b06-bc69-47b34d546a97",
};
```

## Fields

| Field                     | Type                                                                                                                                               | Required           | Description                                                                          |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------ |
| `audience`                | _string_                                                                                                                                           | :heavy_minus_sign: | Replace the upstream OAuth audience sent for this client. Omit to leave unchanged.   |
| `clientSecret`            | _string_                                                                                                                                           | :heavy_minus_sign: | Rotate the client secret. Gram re-encrypts before persisting.                        |
| `id`                      | _string_                                                                                                                                           | :heavy_check_mark: | The remote_session_client id.                                                        |
| `scope`                   | _string_[]                                                                                                                                         | :heavy_minus_sign: | Replace the explicit upstream OAuth scopes for this client. Omit to leave unchanged. |
| `tokenEndpointAuthMethod` | [components.UpdateRemoteSessionClientFormTokenEndpointAuthMethod](../../models/components/updateremotesessionclientformtokenendpointauthmethod.md) | :heavy_minus_sign: | Change how the client authenticates at the issuer's token endpoint.                  |
