# ListRemoteSessionClientsRequest

## Example Usage

```typescript
import { ListRemoteSessionClientsRequest } from "@gram/client/models/operations/listremotesessionclients.js";

let value: ListRemoteSessionClientsRequest = {};
```

## Fields

| Field                   | Type     | Required           | Description                                             |
| ----------------------- | -------- | ------------------ | ------------------------------------------------------- |
| `remoteSessionIssuerId` | _string_ | :heavy_minus_sign: | Filter to clients registered with this issuer.          |
| `userSessionIssuerId`   | _string_ | :heavy_minus_sign: | Filter to clients paired with this user_session_issuer. |
| `cursor`                | _string_ | :heavy_minus_sign: | Pagination cursor.                                      |
| `limit`                 | _number_ | :heavy_minus_sign: | Page size (default 50, max 100).                        |
| `gramSession`           | _string_ | :heavy_minus_sign: | Session header                                          |
| `gramKey`               | _string_ | :heavy_minus_sign: | API Key header                                          |
| `gramProject`           | _string_ | :heavy_minus_sign: | project header                                          |
