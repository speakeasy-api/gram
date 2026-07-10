# CreateOtelForwardingDestinationRequest

## Example Usage

```typescript
import { CreateOtelForwardingDestinationRequest } from "@gram/client/models/operations";

let value: CreateOtelForwardingDestinationRequest = {
  createDestinationRequestBody: {
    enabled: false,
    endpointUrl: "https://gorgeous-opera.info",
    name: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `createDestinationRequestBody`                                                                     | [components.CreateDestinationRequestBody](../../models/components/createdestinationrequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |