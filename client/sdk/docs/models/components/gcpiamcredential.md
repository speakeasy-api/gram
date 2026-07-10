# GcpIamCredential

A GCP IAM external credential.

## Example Usage

```typescript
import { GcpIamCredential } from "@gram/client/models/components/gcpiamcredential.js";

let value: GcpIamCredential = {
  createdAt: new Date("2024-04-06T14:23:25.288Z"),
  id: "c5fdc594-e274-4b6a-8604-a310db1dc299",
  name: "<value>",
  organizationId: "<id>",
  provider: "gcp_iam",
  updatedAt: new Date("2025-01-05T00:44:49.443Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the credential was created.                                                              |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the external credential.                                                            |
| `impersonateServiceAccount`                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The service account Gram impersonates (impersonation approach, or the WIF hop).               |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A human-readable name for the credential.                                                     |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization that owns the credential.                                                    |
| `provider`                                                                                    | [components.GcpIamCredentialProvider](../../models/components/gcpiamcredentialprovider.md)    | :heavy_check_mark:                                                                            | The cloud provider of the credential.                                                         |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the credential was last updated.                                                         |
| `wifPoolId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | Workload Identity Federation pool ID.                                                         |
| `wifProjectNumber`                                                                            | *string*                                                                                      | :heavy_minus_sign:                                                                            | GCP project number backing the WIF pool.                                                      |
| `wifProviderId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Workload Identity Federation provider ID.                                                     |