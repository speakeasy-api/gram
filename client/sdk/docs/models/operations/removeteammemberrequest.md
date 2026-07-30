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

| Field            | Type     | Required           | Description                  |
| ---------------- | -------- | ------------------ | ---------------------------- |
| `organizationId` | _string_ | :heavy_check_mark: | The ID of the organization   |
| `userId`         | _string_ | :heavy_check_mark: | The ID of the user to remove |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header               |
