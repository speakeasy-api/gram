# RiskOverviewUser

## Example Usage

```typescript
import { RiskOverviewUser } from "@gram/client/models/components/riskoverviewuser.js";

let value: RiskOverviewUser = {
  email: "Jacquelyn.Padberg@hotmail.com",
  externalUserId: "<id>",
  findings: 938606,
};
```

## Fields

| Field                                                                                                                       | Type                                                                                                                        | Required                                                                                                                    | Description                                                                                                                 |
| --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `email`                                                                                                                     | *string*                                                                                                                    | :heavy_check_mark:                                                                                                          | User email, or Unknown user when unavailable.                                                                               |
| `externalUserId`                                                                                                            | *string*                                                                                                                    | :heavy_check_mark:                                                                                                          | External user identifier as recorded on chats, when known. Empty when the finding cannot be attributed to an external user. |
| `findings`                                                                                                                  | *number*                                                                                                                    | :heavy_check_mark:                                                                                                          | Finding count for this user.                                                                                                |