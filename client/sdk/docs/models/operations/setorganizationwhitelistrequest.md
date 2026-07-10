# SetOrganizationWhitelistRequest

## Example Usage

```typescript
import { SetOrganizationWhitelistRequest } from "@gram/client/models/operations/setorganizationwhitelist.js";

let value: SetOrganizationWhitelistRequest = {
  setOrganizationWhitelistRequestBody: {
    organizationId: "<id>",
    whitelisted: true,
  },
};
```

## Fields

| Field                                 | Type                                                                                                             | Required           | Description    |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                             | _string_                                                                                                         | :heavy_minus_sign: | API Key header |
| `setOrganizationWhitelistRequestBody` | [components.SetOrganizationWhitelistRequestBody](../../models/components/setorganizationwhitelistrequestbody.md) | :heavy_check_mark: | N/A            |
