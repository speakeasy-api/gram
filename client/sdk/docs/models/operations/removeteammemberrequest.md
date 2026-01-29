# RemoveTeamMemberRequest

## Example Usage

```typescript
import { RemoveTeamMemberRequest } from "@gram/client/models/operations";

let value: RemoveTeamMemberRequest = {
  organizationId: "<id>",
  userId: "<id>",
};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `organizationId`             | *string*                     | :heavy_check_mark:           | The ID of the organization   |
| `userId`                     | *string*                     | :heavy_check_mark:           | The ID of the user to remove |
| `gramSession`                | *string*                     | :heavy_minus_sign:           | Session header               |