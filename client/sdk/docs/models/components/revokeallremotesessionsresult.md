# RevokeAllRemoteSessionsResult

Result type for revoking all of a client's remote_sessions.

## Example Usage

```typescript
import { RevokeAllRemoteSessionsResult } from "@gram/client/models/components/revokeallremotesessionsresult.js";

let value: RevokeAllRemoteSessionsResult = {
  revokedCount: 356623,
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `revokedCount`                     | *number*                           | :heavy_check_mark:                 | Number of remote_sessions revoked. |