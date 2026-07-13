# RevokeAllOrganizationRemoteSessionClientSessionsRequest

## Example Usage

```typescript
import { RevokeAllOrganizationRemoteSessionClientSessionsRequest } from "@gram/client/models/operations/revokeallorganizationremotesessionclientsessions.js";

let value: RevokeAllOrganizationRemoteSessionClientSessionsRequest = {
  clientId: "e880374a-5327-4032-bc18-1b640396ae78",
};
```

## Fields

| Field         | Type     | Required           | Description                   |
| ------------- | -------- | ------------------ | ----------------------------- |
| `clientId`    | _string_ | :heavy_check_mark: | The remote_session_client id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                |
