# SetBillingMetadataRequest

## Example Usage

```typescript
import { SetBillingMetadataRequest } from "@gram/client/models/operations/setbillingmetadata.js";

let value: SetBillingMetadataRequest = {
  setBillingMetadataRequestBody: {
    billingCycleAnchorDay: 251084,
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `setBillingMetadataRequestBody`                                                                      | [components.SetBillingMetadataRequestBody](../../models/components/setbillingmetadatarequestbody.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |