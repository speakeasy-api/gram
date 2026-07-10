# GetRemoteSessionIssuerRequest

## Example Usage

```typescript
import { GetRemoteSessionIssuerRequest } from "@gram/client/models/operations/getremotesessionissuer.js";

let value: GetRemoteSessionIssuerRequest = {};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `id`                            | *string*                        | :heavy_minus_sign:              | The remote_session_issuer id.   |
| `slug`                          | *string*                        | :heavy_minus_sign:              | The remote_session_issuer slug. |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |
| `gramKey`                       | *string*                        | :heavy_minus_sign:              | API Key header                  |
| `gramProject`                   | *string*                        | :heavy_minus_sign:              | project header                  |