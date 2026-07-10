# DeleteRemoteSessionClientRequest

## Example Usage

```typescript
import { DeleteRemoteSessionClientRequest } from "@gram/client/models/operations/deleteremotesessionclient.js";

let value: DeleteRemoteSessionClientRequest = {
  id: "46bc438a-7cc0-4b8b-8da6-4b263a011761",
};
```

## Fields

| Field         | Type     | Required           | Description                   |
| ------------- | -------- | ------------------ | ----------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The remote_session_client id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                |
