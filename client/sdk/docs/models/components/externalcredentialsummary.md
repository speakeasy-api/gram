# ExternalCredentialSummary

Provider-independent summary of an external credential.

## Example Usage

```typescript
import { ExternalCredentialSummary } from "@gram/client/models/components/externalcredentialsummary.js";

let value: ExternalCredentialSummary = {
  createdAt: new Date("2026-09-22T14:41:21.753Z"),
  id: "0f5a1d45-b4a1-4779-8442-9fd04929f6d3",
  name: "<value>",
  organizationId: "<id>",
  provider: "gcp_iam",
  updatedAt: new Date("2024-11-15T05:28:45.177Z"),
};
```

## Fields

| Field            | Type                                                                                                         | Required           | Description                                |
| ---------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------ |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                | :heavy_check_mark: | When the credential was created.           |
| `id`             | _string_                                                                                                     | :heavy_check_mark: | The ID of the external credential.         |
| `name`           | _string_                                                                                                     | :heavy_check_mark: | A human-readable name for the credential.  |
| `organizationId` | _string_                                                                                                     | :heavy_check_mark: | The organization that owns the credential. |
| `provider`       | [components.ExternalCredentialSummaryProvider](../../models/components/externalcredentialsummaryprovider.md) | :heavy_check_mark: | The cloud provider of the credential.      |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                | :heavy_check_mark: | When the credential was last updated.      |
