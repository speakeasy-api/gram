# UpsertConfigRequestBody

## Example Usage

```typescript
import { UpsertConfigRequestBody } from "@gram/client/models/components/upsertconfigrequestbody.js";

let value: UpsertConfigRequestBody = {
  apiKey: "<value>",
  enabled: false,
  provider: "<value>",
};
```

## Fields

| Field                    | Type      | Required           | Description                                                                                                                  |
| ------------------------ | --------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| `apiKey`                 | _string_  | :heavy_check_mark: | Provider API key. Stored encrypted at rest; never returned on reads.                                                         |
| `billingMode`            | _string_  | :heavy_minus_sign: | How the provider org is billed: 'metered', 'flat_rate', or 'unknown'. Free-form; omit to leave the existing value unchanged. |
| `enabled`                | _boolean_ | :heavy_check_mark: | Whether the integration should be active.                                                                                    |
| `externalOrganizationId` | _string_  | :heavy_minus_sign: | Provider organization identifier. Required for anthropic_compliance and codex_compliance.                                    |
| `provider`               | _string_  | :heavy_check_mark: | AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.                         |
