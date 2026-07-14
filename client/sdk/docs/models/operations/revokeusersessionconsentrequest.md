# RevokeUserSessionConsentRequest

## Example Usage

```typescript
import { RevokeUserSessionConsentRequest } from "@gram/client/models/operations/revokeusersessionconsent.js";

let value: RevokeUserSessionConsentRequest = {
  id: "0faca1d2-b7bf-4fb1-8ab7-6058a001efce",
};
```

## Fields

| Field         | Type     | Required           | Description                  |
| ------------- | -------- | ------------------ | ---------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The user_session_consent id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header               |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header               |
| `gramProject` | _string_ | :heavy_minus_sign: | project header               |
