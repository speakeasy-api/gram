# RemovePrincipalGrantsRequest

## Example Usage

```typescript
import { RemovePrincipalGrantsRequest } from "@gram/client/models/operations";

let value: RemovePrincipalGrantsRequest = {
  removePrincipalGrantsRequestBody: {
    principalUrn: "<value>",
  },
};
```

## Fields

| Field                              | Type                                                                                                       | Required           | Description    |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                          | _string_                                                                                                   | :heavy_minus_sign: | API Key header |
| `gramSession`                      | _string_                                                                                                   | :heavy_minus_sign: | Session header |
| `removePrincipalGrantsRequestBody` | [components.RemovePrincipalGrantsRequestBody](../../models/components/removeprincipalgrantsrequestbody.md) | :heavy_check_mark: | N/A            |
