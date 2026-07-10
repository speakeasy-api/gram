# SessionInfoResponse

## Example Usage

```typescript
import { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";

let value: SessionInfoResponse = {
  headers: {},
  result: {
    activeOrganizationId: "<id>",
    gramAccountType: "<value>",
    hasActiveSubscription: true,
    isAdmin: false,
    organizations: [],
    userEmail: "<value>",
    userId: "<id>",
    whitelisted: false,
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `headers`                                                                  | Record<string, *string*[]>                                                 | :heavy_check_mark:                                                         | N/A                                                                        |
| `result`                                                                   | [components.InfoResponseBody](../../models/components/inforesponsebody.md) | :heavy_check_mark:                                                         | N/A                                                                        |