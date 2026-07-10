# OrganizationRemoteSessionClient

An organization-administrator view of a remote_session_client: the client plus the number of MCP servers it is attached to and the number of active sessions minted against it.

## Example Usage

```typescript
import { OrganizationRemoteSessionClient } from "@gram/client/models/components/organizationremotesessionclient.js";

let value: OrganizationRemoteSessionClient = {
  activeSessionCount: 320176,
  client: {
    clientId: "<id>",
    clientIdIssuedAt: new Date("2026-09-14T23:56:25.839Z"),
    createdAt: new Date("2024-06-16T17:20:40.370Z"),
    id: "1ad54e20-113e-4b1b-9d54-7427ac94b437",
    organizationId: "<id>",
    projectId: "<id>",
    remoteSessionIssuerId: "305a7b78-6baf-4ea4-a8a5-3e21532ec4e7",
    updatedAt: new Date("2024-02-17T09:35:45.847Z"),
    userSessionIssuerIds: [
      "1febab0c-3cbc-4bd0-a13a-1cfcfd8a38ce",
      "03cdab4c-a7cf-4fc0-97b4-c402e2852231",
      "414bd070-9024-460f-921b-59671791d034",
    ],
  },
  mcpServerCount: 650776,
};
```

## Fields

| Field                | Type                                                                             | Required           | Description                                                                           |
| -------------------- | -------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------- |
| `activeSessionCount` | _number_                                                                         | :heavy_check_mark: | Number of non-deleted (active) remote_sessions minted against this client.            |
| `client`             | [components.RemoteSessionClient](../../models/components/remotesessionclient.md) | :heavy_check_mark: | A remote_session_client record. client_secret_encrypted is never returned.            |
| `mcpServerCount`     | _number_                                                                         | :heavy_check_mark: | Number of non-deleted MCP servers attached to this client (via user_session_issuers). |
