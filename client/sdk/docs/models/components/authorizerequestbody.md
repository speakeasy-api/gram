# AuthorizeRequestBody

## Example Usage

```typescript
import { AuthorizeRequestBody } from "@gram/client/models/components/authorizerequestbody.js";

let value: AuthorizeRequestBody = {
  codeChallenge: "<value>",
  codeChallengeMethod: "S256",
};
```

## Fields

| Field                                                                                                         | Type                                                                                                          | Required                                                                                                      | Description                                                                                                   |
| ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `codeChallenge`                                                                                               | *string*                                                                                                      | :heavy_check_mark:                                                                                            | PKCE code challenge: base64url(sha256(code_verifier)).                                                        |
| `codeChallengeMethod`                                                                                         | [components.CodeChallengeMethod](../../models/components/codechallengemethod.md)                              | :heavy_check_mark:                                                                                            | PKCE challenge method. Only S256 is supported.                                                                |
| `projectSlug`                                                                                                 | *string*                                                                                                      | :heavy_minus_sign:                                                                                            | Optional project slug to scope the minted key to. Defaults to the org's default (first) project when omitted. |