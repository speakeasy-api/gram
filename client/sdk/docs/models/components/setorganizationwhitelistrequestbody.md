# SetOrganizationWhitelistRequestBody

## Example Usage

```typescript
import { SetOrganizationWhitelistRequestBody } from "@gram/client/models/components/setorganizationwhitelistrequestbody.js";

let value: SetOrganizationWhitelistRequestBody = {
  organizationId: "<id>",
  whitelisted: true,
};
```

## Fields

| Field            | Type      | Required           | Description                                    |
| ---------------- | --------- | ------------------ | ---------------------------------------------- |
| `organizationId` | _string_  | :heavy_check_mark: | The ID of the organization to update           |
| `whitelisted`    | _boolean_ | :heavy_check_mark: | Whether the organization should be whitelisted |
