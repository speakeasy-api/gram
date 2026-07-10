# RoleSummary

Aggregated usage summary for a role

## Example Usage

```typescript
import { RoleSummary } from "@gram/client/models/components/rolesummary.js";

let value: RoleSummary = {
  costPerUser: 7563.29,
  roleId: "<id>",
  roleName: "<value>",
  totalChats: 280670,
  totalCost: 4150.67,
  totalInputTokens: 221330,
  totalOutputTokens: 942549,
  totalTokens: 683258,
  userCount: 117763,
};
```

## Fields

| Field                                           | Type                                            | Required                                        | Description                                     |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `costPerUser`                                   | *number*                                        | :heavy_check_mark:                              | Average cost per user (total_cost / user_count) |
| `roleId`                                        | *string*                                        | :heavy_check_mark:                              | Role identifier extracted from role URN         |
| `roleName`                                      | *string*                                        | :heavy_check_mark:                              | Human-readable role name                        |
| `totalChats`                                    | *number*                                        | :heavy_check_mark:                              | Total chat sessions across all users            |
| `totalCost`                                     | *number*                                        | :heavy_check_mark:                              | Total cost across all users with this role      |
| `totalInputTokens`                              | *number*                                        | :heavy_check_mark:                              | Sum of input tokens across all users            |
| `totalOutputTokens`                             | *number*                                        | :heavy_check_mark:                              | Sum of output tokens across all users           |
| `totalTokens`                                   | *number*                                        | :heavy_check_mark:                              | Sum of all tokens across all users              |
| `userCount`                                     | *number*                                        | :heavy_check_mark:                              | Number of users with this role                  |