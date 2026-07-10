# SearchUsersResult

Result of searching user usage summaries

## Example Usage

```typescript
import { SearchUsersResult } from "@gram/client/models/components/searchusersresult.js";

let value: SearchUsersResult = {
  users: [],
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `nextCursor`                                                       | *string*                                                           | :heavy_minus_sign:                                                 | Cursor for next page                                               |
| `roles`                                                            | [components.RoleSummary](../../models/components/rolesummary.md)[] | :heavy_minus_sign:                                                 | List of role usage summaries (populated when group_by=role)        |
| `users`                                                            | [components.UserSummary](../../models/components/usersummary.md)[] | :heavy_check_mark:                                                 | List of user usage summaries (populated when group_by=employee)    |