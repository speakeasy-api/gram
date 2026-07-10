# UpsertAIIntegrationConfigRequest

## Example Usage

```typescript
import { UpsertAIIntegrationConfigRequest } from "@gram/client/models/components";

let value: UpsertAIIntegrationConfigRequest = {
  apiKey: "<value>",
  enabled: true,
  provider: "<value>",
};
```

## Fields

| Field                                                                             | Type                                                                              | Required                                                                          | Description                                                                       |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `apiKey`                                                                          | *string*                                                                          | :heavy_check_mark:                                                                | Provider API key. Stored encrypted at rest; never returned on reads.              |
| `enabled`                                                                         | *boolean*                                                                         | :heavy_check_mark:                                                                | Whether the integration should be active.                                         |
| `externalOrganizationId`                                                          | *string*                                                                          | :heavy_minus_sign:                                                                | Provider organization identifier. Required for anthropic_compliance.              |
| `provider`                                                                        | *string*                                                                          | :heavy_check_mark:                                                                | AI provider identifier. Supported values include cursor and anthropic_compliance. |