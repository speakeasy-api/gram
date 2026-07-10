# DeleteAIIntegrationConfigRequest

## Example Usage

```typescript
import { DeleteAIIntegrationConfigRequest } from "@gram/client/models/operations/deleteaiintegrationconfig.js";

let value: DeleteAIIntegrationConfigRequest = {
  deleteConfigRequestBody: {
    provider: "<value>",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramKey`                                                                                | *string*                                                                                 | :heavy_minus_sign:                                                                       | API Key header                                                                           |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `deleteConfigRequestBody`                                                                | [components.DeleteConfigRequestBody](../../models/components/deleteconfigrequestbody.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |