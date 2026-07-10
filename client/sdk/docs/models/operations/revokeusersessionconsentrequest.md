# RevokeUserSessionConsentRequest

## Example Usage

```typescript
import { RevokeUserSessionConsentRequest } from "@gram/client/models/operations/revokeusersessionconsent.js";

let value: RevokeUserSessionConsentRequest = {
  id: "0faca1d2-b7bf-4fb1-8ab7-6058a001efce",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `id`                         | *string*                     | :heavy_check_mark:           | The user_session_consent id. |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |
| `gramKey`                    | *string*                     | :heavy_minus_sign:           | API Key header               |
| `gramProject`                | *string*                     | :heavy_minus_sign:           | project header               |