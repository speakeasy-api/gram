# MoveOrganizationRemoteSessionIssuerRequest

## Example Usage

```typescript
import { MoveOrganizationRemoteSessionIssuerRequest } from "@gram/client/models/operations/moveorganizationremotesessionissuer.js";

let value: MoveOrganizationRemoteSessionIssuerRequest = {
  moveIssuerRequestBody: {
    id: "52a1f6fc-fd3e-4234-bca4-0a3f1db467de",
  },
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `gramSession`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | Session header                                                                       |
| `gramKey`                                                                            | *string*                                                                             | :heavy_minus_sign:                                                                   | API Key header                                                                       |
| `moveIssuerRequestBody`                                                              | [components.MoveIssuerRequestBody](../../models/components/moveissuerrequestbody.md) | :heavy_check_mark:                                                                   | N/A                                                                                  |