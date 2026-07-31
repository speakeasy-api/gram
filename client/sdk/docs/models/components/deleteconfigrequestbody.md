# DeleteConfigRequestBody

## Example Usage

```typescript
import { DeleteConfigRequestBody } from "@gram/client/models/components/deleteconfigrequestbody.js";

let value: DeleteConfigRequestBody = {
  provider: "<value>",
};
```

## Fields

| Field      | Type     | Required           | Description                                                                                          |
| ---------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------- |
| `provider` | _string_ | :heavy_check_mark: | AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance. |
