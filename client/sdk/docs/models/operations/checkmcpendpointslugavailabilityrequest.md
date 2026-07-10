# CheckMcpEndpointSlugAvailabilityRequest

## Example Usage

```typescript
import { CheckMcpEndpointSlugAvailabilityRequest } from "@gram/client/models/operations/checkmcpendpointslugavailability.js";

let value: CheckMcpEndpointSlugAvailabilityRequest = {
  slug: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                                                 |
| ---------------- | -------- | ------------------ | --------------------------------------------------------------------------- |
| `slug`           | _string_ | :heavy_check_mark: | The slug to check                                                           |
| `customDomainId` | _string_ | :heavy_minus_sign: | Optional custom domain ID. Omit to check platform-domain slug availability. |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header                                                              |
| `gramKey`        | _string_ | :heavy_minus_sign: | API Key header                                                              |
| `gramProject`    | _string_ | :heavy_minus_sign: | project header                                                              |
