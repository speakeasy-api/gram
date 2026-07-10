# RevokeUserSessionRequest

## Example Usage

```typescript
import { RevokeUserSessionRequest } from "@gram/client/models/operations/revokeusersession.js";

let value: RevokeUserSessionRequest = {
  id: "c3b2ea70-ad9a-488a-bbd1-5e013f8f4965",
};
```

## Fields

| Field                | Type                 | Required             | Description          |
| -------------------- | -------------------- | -------------------- | -------------------- |
| `id`                 | *string*             | :heavy_check_mark:   | The user_session id. |
| `gramSession`        | *string*             | :heavy_minus_sign:   | Session header       |
| `gramKey`            | *string*             | :heavy_minus_sign:   | API Key header       |
| `gramProject`        | *string*             | :heavy_minus_sign:   | project header       |