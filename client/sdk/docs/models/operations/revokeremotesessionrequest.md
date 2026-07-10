# RevokeRemoteSessionRequest

## Example Usage

```typescript
import { RevokeRemoteSessionRequest } from "@gram/client/models/operations/revokeremotesession.js";

let value: RevokeRemoteSessionRequest = {
  id: "9a0c14fe-a1d6-48d4-b035-0b0dea7f75ae",
};
```

## Fields

| Field                  | Type                   | Required               | Description            |
| ---------------------- | ---------------------- | ---------------------- | ---------------------- |
| `id`                   | *string*               | :heavy_check_mark:     | The remote_session id. |
| `gramSession`          | *string*               | :heavy_minus_sign:     | Session header         |
| `gramKey`              | *string*               | :heavy_minus_sign:     | API Key header         |
| `gramProject`          | *string*               | :heavy_minus_sign:     | project header         |