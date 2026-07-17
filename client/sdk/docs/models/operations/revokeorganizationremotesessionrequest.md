# RevokeOrganizationRemoteSessionRequest

## Example Usage

```typescript
import { RevokeOrganizationRemoteSessionRequest } from "@gram/client/models/operations/revokeorganizationremotesession.js";

let value: RevokeOrganizationRemoteSessionRequest = {
  id: "d153292e-c68b-4f0f-9db5-783f44e3ef63",
};
```

## Fields

| Field         | Type     | Required           | Description            |
| ------------- | -------- | ------------------ | ---------------------- |
| `id`          | _string_ | :heavy_check_mark: | The remote_session id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header         |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header         |
