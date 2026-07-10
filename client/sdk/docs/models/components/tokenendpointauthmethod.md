# TokenEndpointAuthMethod

How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.

## Example Usage

```typescript
import { TokenEndpointAuthMethod } from "@gram/client/models/components/createglobalremotesessionclientform.js";

let value: TokenEndpointAuthMethod = "client_secret_basic";
```

## Values

```typescript
"client_secret_basic" | "client_secret_post" | "none";
```
