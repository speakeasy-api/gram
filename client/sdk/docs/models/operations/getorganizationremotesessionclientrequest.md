# GetOrganizationRemoteSessionClientRequest

## Example Usage

```typescript
import { GetOrganizationRemoteSessionClientRequest } from "@gram/client/models/operations/getorganizationremotesessionclient.js";

let value: GetOrganizationRemoteSessionClientRequest = {
  id: "84c048e2-3874-4c92-8b0f-11835193cda6",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_client id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |