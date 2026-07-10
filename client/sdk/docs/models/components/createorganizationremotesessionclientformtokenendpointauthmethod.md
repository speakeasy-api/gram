# CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod

How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.

## Example Usage

```typescript
import { CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createorganizationremotesessionclientform.js";

let value: CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod =
  "none";
```

## Values

```typescript
"client_secret_basic" | "client_secret_post" | "none"
```