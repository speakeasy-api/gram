# RedeemRequestBody

## Example Usage

```typescript
import { RedeemRequestBody } from "@gram/client/models/components/redeemrequestbody.js";

let value: RedeemRequestBody = {
  code: "<value>",
  codeVerifier: "<value>",
};
```

## Fields

| Field                                                                                 | Type                                                                                  | Required                                                                              | Description                                                                           |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `code`                                                                                | *string*                                                                              | :heavy_check_mark:                                                                    | The opaque one-time code issued by authorize.                                         |
| `codeVerifier`                                                                        | *string*                                                                              | :heavy_check_mark:                                                                    | The PKCE code verifier whose base64url(sha256(...)) equals the stored code_challenge. |