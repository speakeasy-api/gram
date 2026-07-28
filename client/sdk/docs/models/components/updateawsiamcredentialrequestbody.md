# UpdateAwsIamCredentialRequestBody

## Example Usage

```typescript
import { UpdateAwsIamCredentialRequestBody } from "@gram/client/models/components/updateawsiamcredentialrequestbody.js";

let value: UpdateAwsIamCredentialRequestBody = {
  id: "7e3f9faf-2280-4a4c-9b63-5f33b309d957",
  name: "<value>",
};
```

## Fields

| Field           | Type     | Required           | Description                                                                               |
| --------------- | -------- | ------------------ | ----------------------------------------------------------------------------------------- |
| `assumeRoleArn` | _string_ | :heavy_minus_sign: | The customer IAM role ARN Gram assumes. Omit for a KMS key-policy grant.                  |
| `id`            | _string_ | :heavy_check_mark: | The ID of the credential to update.                                                       |
| `name`          | _string_ | :heavy_check_mark: | A human-readable name for the credential.                                                 |
| `oidcAudience`  | _string_ | :heavy_minus_sign: | The OIDC audience. Provide (with assume_role_arn) to assume the role with a web identity. |
| `oidcSubject`   | _string_ | :heavy_minus_sign: | Optional OIDC subject pin; only valid alongside oidc_audience.                            |
| `stsRegion`     | _string_ | :heavy_minus_sign: | Optional STS region override.                                                             |
