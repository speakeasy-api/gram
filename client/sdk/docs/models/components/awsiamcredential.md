# AwsIamCredential

An AWS IAM external credential.

## Example Usage

```typescript
import { AwsIamCredential } from "@gram/client/models/components/awsiamcredential.js";

let value: AwsIamCredential = {
  createdAt: new Date("2024-07-05T19:04:13.198Z"),
  id: "a7f25dda-f08a-4979-bab9-f8a9c640c88f",
  name: "<value>",
  organizationId: "<id>",
  provider: "aws_iam",
  updatedAt: new Date("2026-03-15T16:48:16.893Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                                                                                |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `assumeRoleArn`  | _string_                                                                                      | :heavy_minus_sign: | The customer IAM role ARN Gram assumes.                                                                                                    |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the credential was created.                                                                                                           |
| `externalId`     | _string_                                                                                      | :heavy_minus_sign: | The Gram-generated ExternalId the customer must require in their role trust policy. Present when Gram assumes the role with an ExternalId. |
| `id`             | _string_                                                                                      | :heavy_check_mark: | The ID of the external credential.                                                                                                         |
| `name`           | _string_                                                                                      | :heavy_check_mark: | A human-readable name for the credential.                                                                                                  |
| `oidcAudience`   | _string_                                                                                      | :heavy_minus_sign: | The OIDC audience. Present when Gram assumes the role with a web identity.                                                                 |
| `oidcSubject`    | _string_                                                                                      | :heavy_minus_sign: | Optional OIDC subject pin (web-identity approach).                                                                                         |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | The organization that owns the credential.                                                                                                 |
| `provider`       | [components.Provider](../../models/components/provider.md)                                    | :heavy_check_mark: | The cloud provider of the credential.                                                                                                      |
| `stsRegion`      | _string_                                                                                      | :heavy_minus_sign: | Optional STS region override.                                                                                                              |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the credential was last updated.                                                                                                      |
