# GetUserSessionClientRequest

## Example Usage

```typescript
import { GetUserSessionClientRequest } from "@gram/client/models/operations/getusersessionclient.js";

let value: GetUserSessionClientRequest = {
  id: "cf697928-ed3c-4d57-b259-052343dcb7bb",
};
```

## Fields

| Field                       | Type                        | Required                    | Description                 |
| --------------------------- | --------------------------- | --------------------------- | --------------------------- |
| `id`                        | *string*                    | :heavy_check_mark:          | The user_session_client id. |
| `gramSession`               | *string*                    | :heavy_minus_sign:          | Session header              |
| `gramKey`                   | *string*                    | :heavy_minus_sign:          | API Key header              |
| `gramProject`               | *string*                    | :heavy_minus_sign:          | project header              |