# RemoteSessionClientTokenEndpointAuthMethod

How the client authenticates at the issuer's token endpoint. Null resolves to client_secret_basic at runtime.

## Example Usage

```typescript
import { RemoteSessionClientTokenEndpointAuthMethod } from "@gram/client/models/components/remotesessionclient.js";

let value: RemoteSessionClientTokenEndpointAuthMethod = "client_secret_post";
```

## Values

```typescript
"client_secret_basic" | "client_secret_post" | "none";
```
