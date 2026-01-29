# ListTeamMembersRequest

## Example Usage

```typescript
import { ListTeamMembersRequest } from "@gram/client/models/operations";

let value: ListTeamMembersRequest = {
  organizationId: "<id>",
};
```

## Fields

| Field                      | Type                       | Required                   | Description                |
| -------------------------- | -------------------------- | -------------------------- | -------------------------- |
| `organizationId`           | *string*                   | :heavy_check_mark:         | The ID of the organization |
| `gramSession`              | *string*                   | :heavy_minus_sign:         | Session header             |