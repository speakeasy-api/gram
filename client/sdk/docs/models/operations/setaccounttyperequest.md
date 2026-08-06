# SetAccountTypeRequest

## Example Usage

```typescript
import { SetAccountTypeRequest } from "@gram/client/models/operations";

let value: SetAccountTypeRequest = {
  setAccountTypeRequestBody: {
    gramAccountType: "free",
    organizationId: "<id>",
  },
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                    | *string*                                                                                     | :heavy_minus_sign:                                                                           | API Key header                                                                               |
| `setAccountTypeRequestBody`                                                                  | [components.SetAccountTypeRequestBody](../../models/components/setaccounttyperequestbody.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |