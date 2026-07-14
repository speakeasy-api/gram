# RevokeUserSessionClientRequest

## Example Usage

```typescript
import { RevokeUserSessionClientRequest } from "@gram/client/models/operations/revokeusersessionclient.js";

let value: RevokeUserSessionClientRequest = {
  id: "3d1a570f-9dc3-4a72-969d-df1324a09cfe",
};
```

## Fields

| Field         | Type     | Required           | Description                 |
| ------------- | -------- | ------------------ | --------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The user_session_client id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header              |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header              |
| `gramProject` | _string_ | :heavy_minus_sign: | project header              |
