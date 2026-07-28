# UpdateGcpIamCredentialRequestBody

## Example Usage

```typescript
import { UpdateGcpIamCredentialRequestBody } from "@gram/client/models/components/updategcpiamcredentialrequestbody.js";

let value: UpdateGcpIamCredentialRequestBody = {
  id: "2a9f99b6-d161-4b47-8e43-0875dc43f1b8",
  name: "<value>",
};
```

## Fields

| Field                       | Type     | Required           | Description                                                                                                            |
| --------------------------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `id`                        | _string_ | :heavy_check_mark: | The ID of the credential to update.                                                                                    |
| `impersonateServiceAccount` | _string_ | :heavy_minus_sign: | The service account Gram impersonates. Set alone for direct impersonation, or as the hop alongside the wif\_\* fields. |
| `name`                      | _string_ | :heavy_check_mark: | A human-readable name for the credential.                                                                              |
| `wifPoolId`                 | _string_ | :heavy_minus_sign: | Workload Identity Federation pool ID. Set together with the other wif\_\* fields.                                      |
| `wifProjectNumber`          | _string_ | :heavy_minus_sign: | GCP project number backing the WIF pool. Set together with the other wif\_\* fields.                                   |
| `wifProviderId`             | _string_ | :heavy_minus_sign: | Workload Identity Federation provider ID. Set together with the other wif\_\* fields.                                  |
