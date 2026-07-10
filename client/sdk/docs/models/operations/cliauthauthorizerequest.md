# CliAuthAuthorizeRequest

## Example Usage

```typescript
import { CliAuthAuthorizeRequest } from "@gram/client/models/operations/cliauthauthorize.js";

let value: CliAuthAuthorizeRequest = {
  authorizeRequestBody: {
    codeChallenge: "<value>",
    codeChallengeMethod: "S256",
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `gramSession`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | Session header                                                                     |
| `authorizeRequestBody`                                                             | [components.AuthorizeRequestBody](../../models/components/authorizerequestbody.md) | :heavy_check_mark:                                                                 | N/A                                                                                |