# DeleteAIIntegrationConfigRequest

## Example Usage

```typescript
import { DeleteAIIntegrationConfigRequest } from "@gram/client/models/components";

let value: DeleteAIIntegrationConfigRequest = {
  provider: "<value>",
};
```

## Fields

| Field      | Type     | Required           | Description                                                                       |
| ---------- | -------- | ------------------ | --------------------------------------------------------------------------------- |
| `provider` | _string_ | :heavy_check_mark: | AI provider identifier. Supported values include cursor and anthropic_compliance. |
