# ApproveShadowMCPRequest

## Example Usage

```typescript
import { ApproveShadowMCPRequest } from "@gram/client/models/operations";

let value: ApproveShadowMCPRequest = {
  approveShadowMCPRequestBody: {
    match: "<value>",
    policyId: "482f0af4-6aa5-495d-8ca5-a02a7fd80b09",
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |
| `approveShadowMCPRequestBody`                                                                    | [components.ApproveShadowMCPRequestBody](../../models/components/approveshadowmcprequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |