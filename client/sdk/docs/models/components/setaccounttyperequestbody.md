# SetAccountTypeRequestBody

## Example Usage

```typescript
import { SetAccountTypeRequestBody } from "@gram/client/models/components";

let value: SetAccountTypeRequestBody = {
  gramAccountType: "free",
  organizationId: "<id>",
};
```

## Fields

| Field                                                                                                                      | Type                                                                                                                       | Required                                                                                                                   | Description                                                                                                                |
| -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `gramAccountType`                                                                                                          | [components.SetAccountTypeRequestBodyGramAccountType](../../models/components/setaccounttyperequestbodygramaccounttype.md) | :heavy_check_mark:                                                                                                         | The new account tier.                                                                                                      |
| `organizationId`                                                                                                           | *string*                                                                                                                   | :heavy_check_mark:                                                                                                         | The Gram organization ID to update.                                                                                        |