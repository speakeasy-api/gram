# ListUserSessionClientsRequest

## Example Usage

```typescript
import { ListUserSessionClientsRequest } from "@gram/client/models/operations/listusersessionclients.js";

let value: ListUserSessionClientsRequest = {};
```

## Fields

| Field                 | Type     | Required           | Description                                                    |
| --------------------- | -------- | ------------------ | -------------------------------------------------------------- |
| `userSessionIssuerId` | _string_ | :heavy_minus_sign: | Filter to clients registered with this issuer.                 |
| `cursor`              | _string_ | :heavy_minus_sign: | Pagination cursor: id of the last item from the previous page. |
| `limit`               | _number_ | :heavy_minus_sign: | Page size (default 50, max 100).                               |
| `gramSession`         | _string_ | :heavy_minus_sign: | Session header                                                 |
| `gramKey`             | _string_ | :heavy_minus_sign: | API Key header                                                 |
| `gramProject`         | _string_ | :heavy_minus_sign: | project header                                                 |
