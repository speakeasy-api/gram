# UpdateOtelForwardingDestinationRequest

## Example Usage

```typescript
import { UpdateOtelForwardingDestinationRequest } from "@gram/client/models/operations";

let value: UpdateOtelForwardingDestinationRequest = {
  updateDestinationRequestBody: {
    enabled: true,
    endpointUrl: "https://fat-publication.org",
    id: "<id>",
    name: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `updateDestinationRequestBody`                                                                     | [components.UpdateDestinationRequestBody](../../models/components/updatedestinationrequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |