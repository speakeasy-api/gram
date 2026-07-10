# CreateRemoteSessionClientFormTokenEndpointAuthMethod

How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.

## Example Usage

```typescript
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";

let value: CreateRemoteSessionClientFormTokenEndpointAuthMethod =
  "client_secret_post";
```

## Values

```typescript
"client_secret_basic" | "client_secret_post" | "none";
```
