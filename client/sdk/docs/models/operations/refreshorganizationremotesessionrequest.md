# RefreshOrganizationRemoteSessionRequest

## Example Usage

```typescript
import { RefreshOrganizationRemoteSessionRequest } from "@gram/client/models/operations/refreshorganizationremotesession.js";

let value: RefreshOrganizationRemoteSessionRequest = {
  id: "95232d25-f700-4170-b28b-9487677cc1da",
};
```

## Fields

| Field         | Type     | Required           | Description            |
| ------------- | -------- | ------------------ | ---------------------- |
| `id`          | _string_ | :heavy_check_mark: | The remote_session id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header         |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header         |
