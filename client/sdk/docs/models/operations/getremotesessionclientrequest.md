# GetRemoteSessionClientRequest

## Example Usage

```typescript
import { GetRemoteSessionClientRequest } from "@gram/client/models/operations/getremotesessionclient.js";

let value: GetRemoteSessionClientRequest = {
  id: "8adf7e73-260a-4d5e-829e-ff94239b5d30",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The remote_session_client id. |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |
| `gramProject`                 | *string*                      | :heavy_minus_sign:            | project header                |