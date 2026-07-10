# ListRemoteSessionClientsResponse

## Example Usage

```typescript
import { ListRemoteSessionClientsResponse } from "@gram/client/models/operations/listremotesessionclients.js";

let value: ListRemoteSessionClientsResponse = {
  result: {
    items: [
      {
        clientId: "<id>",
        clientIdIssuedAt: new Date("2025-11-25T09:34:25.830Z"),
        createdAt: new Date("2025-02-19T10:14:18.312Z"),
        id: "b64fb671-f106-4fcd-b592-d2af25ccafe6",
        organizationId: "<id>",
        projectId: "<id>",
        remoteSessionIssuerId: "f0ce74cb-c8eb-4080-b92b-140e118dcf3c",
        updatedAt: new Date("2024-09-04T06:33:26.958Z"),
        userSessionIssuerIds: [],
      },
    ],
  },
};
```

## Fields

| Field    | Type                                                                                                   | Required           | Description |
| -------- | ------------------------------------------------------------------------------------------------------ | ------------------ | ----------- |
| `result` | [components.ListRemoteSessionClientsResult](../../models/components/listremotesessionclientsresult.md) | :heavy_check_mark: | N/A         |
