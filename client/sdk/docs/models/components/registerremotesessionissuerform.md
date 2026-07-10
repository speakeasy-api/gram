# RegisterRemoteSessionIssuerForm

Form for registering a new remote_session_client against an existing remote_session_issuer via RFC 7591 Dynamic Client Registration.

## Example Usage

```typescript
import { RegisterRemoteSessionIssuerForm } from "@gram/client/models/components";

let value: RegisterRemoteSessionIssuerForm = {
  remoteSessionIssuerId: "85830354-dde9-44d3-b7f4-27c4d7e4a83f",
  userSessionIssuerId: "a9547f0d-e196-4c3f-9487-3778b584154d",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `clientName`                                                                                 | *string*                                                                                     | :heavy_minus_sign:                                                                           | Optional client_name to send in the RFC 7591 registration request.                           |
| `redirectUris`                                                                               | *string*[]                                                                                   | :heavy_minus_sign:                                                                           | Optional redirect_uris to send in the RFC 7591 registration request.                         |
| `remoteSessionIssuerId`                                                                      | *string*                                                                                     | :heavy_check_mark:                                                                           | The remote_session_issuer to register against. Must have a registration_endpoint configured. |
| `userSessionIssuerId`                                                                        | *string*                                                                                     | :heavy_check_mark:                                                                           | The user_session_issuer the issued client is paired with.                                    |