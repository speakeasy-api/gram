# GetRemoteSessionIssuerRequest

## Example Usage

```typescript
import { GetRemoteSessionIssuerRequest } from "@gram/client/models/operations/getremotesessionissuer.js";

let value: GetRemoteSessionIssuerRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                     |
| ------------- | -------- | ------------------ | ------------------------------- |
| `id`          | _string_ | :heavy_minus_sign: | The remote_session_issuer id.   |
| `slug`        | _string_ | :heavy_minus_sign: | The remote_session_issuer slug. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                  |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                  |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                  |
