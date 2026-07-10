# CreateGcpIamCredentialForm

## Example Usage

```typescript
import { CreateGcpIamCredentialForm } from "@gram/client/models/components/creategcpiamcredentialform.js";

let value: CreateGcpIamCredentialForm = {
  name: "<value>",
};
```

## Fields

| Field                                                                                                                | Type                                                                                                                 | Required                                                                                                             | Description                                                                                                          |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `impersonateServiceAccount`                                                                                          | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | The service account Gram impersonates. Set alone for direct impersonation, or as the hop alongside the wif_* fields. |
| `name`                                                                                                               | *string*                                                                                                             | :heavy_check_mark:                                                                                                   | A human-readable name for the credential.                                                                            |
| `wifPoolId`                                                                                                          | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | Workload Identity Federation pool ID. Set together with the other wif_* fields.                                      |
| `wifProjectNumber`                                                                                                   | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | GCP project number backing the WIF pool. Set together with the other wif_* fields.                                   |
| `wifProviderId`                                                                                                      | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | Workload Identity Federation provider ID. Set together with the other wif_* fields.                                  |