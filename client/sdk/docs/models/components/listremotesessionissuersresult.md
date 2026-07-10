# ListRemoteSessionIssuersResult

Result type for listing remote_session_issuers.

## Example Usage

```typescript
import { ListRemoteSessionIssuersResult } from "@gram/client/models/components/listremotesessionissuersresult.js";

let value: ListRemoteSessionIssuersResult = {
  items: [],
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `items`                                                                            | [components.RemoteSessionIssuer](../../models/components/remotesessionissuer.md)[] | :heavy_check_mark:                                                                 | N/A                                                                                |
| `nextCursor`                                                                       | *string*                                                                           | :heavy_minus_sign:                                                                 | Cursor for the next page; empty when exhausted.                                    |