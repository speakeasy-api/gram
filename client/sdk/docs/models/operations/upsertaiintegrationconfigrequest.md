# UpsertAIIntegrationConfigRequest

## Example Usage

```typescript
import { UpsertAIIntegrationConfigRequest } from "@gram/client/models/operations/upsertaiintegrationconfig.js";

let value: UpsertAIIntegrationConfigRequest = {
  upsertConfigRequestBody: {
    apiKey: "<value>",
    enabled: true,
    provider: "<value>",
  },
};
```

## Fields

| Field                     | Type                                                                                     | Required           | Description    |
| ------------------------- | ---------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                 | _string_                                                                                 | :heavy_minus_sign: | API Key header |
| `gramSession`             | _string_                                                                                 | :heavy_minus_sign: | Session header |
| `upsertConfigRequestBody` | [components.UpsertConfigRequestBody](../../models/components/upsertconfigrequestbody.md) | :heavy_check_mark: | N/A            |
