# GetTeamInviteInfoRequest

## Example Usage

```typescript
import { GetTeamInviteInfoRequest } from "@gram/client/models/operations";

let value: GetTeamInviteInfoRequest = {
  token: "<value>",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `token`                              | *string*                             | :heavy_check_mark:                   | The invite token from the email link |
| `gramSession`                        | *string*                             | :heavy_minus_sign:                   | Session header                       |