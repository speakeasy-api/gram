# ListUserSessionsResult

Result type for listing user_sessions.

## Example Usage

```typescript
import { ListUserSessionsResult } from "@gram/client/models/components/listusersessionsresult.js";

let value: ListUserSessionsResult = {
  items: [],
};
```

## Fields

| Field        | Type                                                               | Required           | Description                                     |
| ------------ | ------------------------------------------------------------------ | ------------------ | ----------------------------------------------- |
| `items`      | [components.UserSession](../../models/components/usersession.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                           | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
