# ListRemoteSessionClientsResult

Result type for listing remote_session_clients.

## Example Usage

```typescript
import { ListRemoteSessionClientsResult } from "@gram/client/models/components/listremotesessionclientsresult.js";

let value: ListRemoteSessionClientsResult = {
  items: [],
};
```

## Fields

| Field        | Type                                                                               | Required           | Description                                     |
| ------------ | ---------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------- |
| `items`      | [components.RemoteSessionClient](../../models/components/remotesessionclient.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                                           | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
