# GetOrganizationRemoteSessionClientDeletePreflightRequest

## Example Usage

```typescript
import { GetOrganizationRemoteSessionClientDeletePreflightRequest } from "@gram/client/models/operations/getorganizationremotesessionclientdeletepreflight.js";

let value: GetOrganizationRemoteSessionClientDeletePreflightRequest = {
  id: "a3a00ce0-6d7c-4a0e-9ff7-c142f8ede2b3",
};
```

## Fields

| Field         | Type     | Required           | Description                   |
| ------------- | -------- | ------------------ | ----------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The remote_session_client id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                |
