# ListTeamMembersRequest

## Example Usage

```typescript
import { ListTeamMembersRequest } from "@gram/client/models/operations";

let value: ListTeamMembersRequest = {
  organizationId: "<id>",
};
```

## Fields

| Field            | Type     | Required           | Description                |
| ---------------- | -------- | ------------------ | -------------------------- |
| `organizationId` | _string_ | :heavy_check_mark: | The ID of the organization |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header             |
