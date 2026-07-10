# ListUserSessionFacetsResult

## Example Usage

```typescript
import { ListUserSessionFacetsResult } from "@gram/client/models/components/listusersessionfacetsresult.js";

let value: ListUserSessionFacetsResult = {
  clients: [],
  servers: [],
  users: [],
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `clients`                                                                                | [components.UserSessionFacetOption](../../models/components/usersessionfacetoption.md)[] | :heavy_check_mark:                                                                       | Connecting client facets.                                                                |
| `servers`                                                                                | [components.UserSessionFacetOption](../../models/components/usersessionfacetoption.md)[] | :heavy_check_mark:                                                                       | Issuer/server facets.                                                                    |
| `users`                                                                                  | [components.UserSessionFacetOption](../../models/components/usersessionfacetoption.md)[] | :heavy_check_mark:                                                                       | Subject (user) facets.                                                                   |