# GetAIIntegrationConfigRequest

## Example Usage

```typescript
import { GetAIIntegrationConfigRequest } from "@gram/client/models/operations/getaiintegrationconfig.js";

let value: GetAIIntegrationConfigRequest = {
  provider: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                                                                       |
| ------------- | -------- | ------------------ | --------------------------------------------------------------------------------- |
| `provider`    | _string_ | :heavy_check_mark: | AI provider identifier. Supported values include cursor and anthropic_compliance. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                                    |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                                    |
