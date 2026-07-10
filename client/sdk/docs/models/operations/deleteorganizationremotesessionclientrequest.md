# DeleteOrganizationRemoteSessionClientRequest

## Example Usage

```typescript
import { DeleteOrganizationRemoteSessionClientRequest } from "@gram/client/models/operations/deleteorganizationremotesessionclient.js";

let value: DeleteOrganizationRemoteSessionClientRequest = {
  id: "6cd44d89-2d45-4c79-b130-23fb53222645",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_client id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |