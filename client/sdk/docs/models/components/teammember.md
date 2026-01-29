# TeamMember

## Example Usage

```typescript
import { TeamMember } from "@gram/client/models/components";

let value: TeamMember = {
  displayName: "Rick_Schmeler24",
  email: "Judah76@gmail.com",
  id: "<id>",
  joinedAt: new Date("2024-06-13T12:34:11.302Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `displayName`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The user's display name                                                                       |
| `email`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The user's email address                                                                      |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The user's ID                                                                                 |
| `joinedAt`                                                                                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the user joined the organization                                                         |
| `photoUrl`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | URL to the user's profile photo                                                               |