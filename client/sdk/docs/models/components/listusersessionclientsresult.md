# ListUserSessionClientsResult

Result type for listing user_session_clients.

## Example Usage

```typescript
import { ListUserSessionClientsResult } from "@gram/client/models/components/listusersessionclientsresult.js";

let value: ListUserSessionClientsResult = {
  items: [
    {
      clientId: "<id>",
      clientIdIssuedAt: new Date("2024-08-08T11:47:50.090Z"),
      clientName: "<value>",
      createdAt: new Date("2025-05-11T21:17:42.235Z"),
      id: "4517f638-52f0-493e-96aa-cf42ec894bd5",
      redirectUris: [],
      updatedAt: new Date("2024-05-10T08:11:14.870Z"),
      userSessionIssuerId: "0435f681-4cbc-4c88-96ab-7b6223d49af0",
    },
  ],
};
```

## Fields

| Field        | Type                                                                           | Required           | Description                                     |
| ------------ | ------------------------------------------------------------------------------ | ------------------ | ----------------------------------------------- |
| `items`      | [components.UserSessionClient](../../models/components/usersessionclient.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                                       | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
