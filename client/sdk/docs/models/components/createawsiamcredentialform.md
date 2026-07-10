# CreateAwsIamCredentialForm

## Example Usage

```typescript
import { CreateAwsIamCredentialForm } from "@gram/client/models/components/createawsiamcredentialform.js";

let value: CreateAwsIamCredentialForm = {
  name: "<value>",
};
```

## Fields

| Field                                                                                     | Type                                                                                      | Required                                                                                  | Description                                                                               |
| ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `assumeRoleArn`                                                                           | *string*                                                                                  | :heavy_minus_sign:                                                                        | The customer IAM role ARN Gram assumes. Omit for a KMS key-policy grant.                  |
| `name`                                                                                    | *string*                                                                                  | :heavy_check_mark:                                                                        | A human-readable name for the credential.                                                 |
| `oidcAudience`                                                                            | *string*                                                                                  | :heavy_minus_sign:                                                                        | The OIDC audience. Provide (with assume_role_arn) to assume the role with a web identity. |
| `oidcSubject`                                                                             | *string*                                                                                  | :heavy_minus_sign:                                                                        | Optional OIDC subject pin; only valid alongside oidc_audience.                            |
| `stsRegion`                                                                               | *string*                                                                                  | :heavy_minus_sign:                                                                        | Optional STS region override.                                                             |