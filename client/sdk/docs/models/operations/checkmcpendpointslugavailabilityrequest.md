# CheckMcpEndpointSlugAvailabilityRequest

## Example Usage

```typescript
import { CheckMcpEndpointSlugAvailabilityRequest } from "@gram/client/models/operations/checkmcpendpointslugavailability.js";

let value: CheckMcpEndpointSlugAvailabilityRequest = {
  slug: "<value>",
};
```

## Fields

| Field                                                                       | Type                                                                        | Required                                                                    | Description                                                                 |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `slug`                                                                      | *string*                                                                    | :heavy_check_mark:                                                          | The slug to check                                                           |
| `customDomainId`                                                            | *string*                                                                    | :heavy_minus_sign:                                                          | Optional custom domain ID. Omit to check platform-domain slug availability. |
| `gramSession`                                                               | *string*                                                                    | :heavy_minus_sign:                                                          | Session header                                                              |
| `gramKey`                                                                   | *string*                                                                    | :heavy_minus_sign:                                                          | API Key header                                                              |
| `gramProject`                                                               | *string*                                                                    | :heavy_minus_sign:                                                          | project header                                                              |