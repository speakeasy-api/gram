# SetBillingMetadataRequestBody

## Example Usage

```typescript
import { SetBillingMetadataRequestBody } from "@gram/client/models/components/setbillingmetadatarequestbody.js";

let value: SetBillingMetadataRequestBody = {
  billingCycleAnchorDay: 92519,
};
```

## Fields

| Field                    | Type     | Required           | Description                                                                                                                              |
| ------------------------ | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `alertEmail`             | _string_ | :heavy_minus_sign: | Email address to notify on TUM threshold events. Omit to clear.                                                                          |
| `billingCycleAnchorDay`  | _number_ | :heavy_check_mark: | Day of month (1-31) the billing cycle starts, at 00:00 UTC                                                                               |
| `monthlyTokenLimit`      | _number_ | :heavy_minus_sign: | The contracted monthly tokens under management limit. Omit to clear.                                                                     |
| `tunneledMcpServerLimit` | _number_ | :heavy_minus_sign: | The contracted tunneled MCP server source cap. Omit to leave the configured value unchanged; never-configured orgs use the plan default. |
