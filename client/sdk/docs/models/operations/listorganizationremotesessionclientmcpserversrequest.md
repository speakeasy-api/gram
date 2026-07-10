# ListOrganizationRemoteSessionClientMcpServersRequest

## Example Usage

```typescript
import { ListOrganizationRemoteSessionClientMcpServersRequest } from "@gram/client/models/operations/listorganizationremotesessionclientmcpservers.js";

let value: ListOrganizationRemoteSessionClientMcpServersRequest = {
  clientId: "f44af85b-edb5-46fa-8613-14756db8d174",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `clientId`                    | *string*                      | :heavy_check_mark:            | The remote_session_client id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |