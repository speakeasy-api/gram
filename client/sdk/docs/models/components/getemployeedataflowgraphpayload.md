# GetEmployeeDataFlowGraphPayload

Payload for getting an employee-level MCP data flow graph

## Example Usage

```typescript
import { GetEmployeeDataFlowGraphPayload } from "@gram/client/models/components/getemployeedataflowgraphpayload.js";

let value: GetEmployeeDataFlowGraphPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                               | Type                                                                                                | Required                                                                                            | Description                                                                                         | Example                                                                                             |
| --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `accountType`                                                                                       | *string*                                                                                            | :heavy_minus_sign:                                                                                  | Optional account type filter ('team' or 'personal')                                                 |                                                                                                     |
| `externalOrgId`                                                                                     | *string*                                                                                            | :heavy_minus_sign:                                                                                  | Optional filter to a single AI account by its provider org id; scopes the graph to that one account |                                                                                                     |
| `externalUserId`                                                                                    | *string*                                                                                            | :heavy_minus_sign:                                                                                  | External user ID to get the graph for (mutually exclusive with user_id)                             |                                                                                                     |
| `from`                                                                                              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)       | :heavy_check_mark:                                                                                  | Start time in ISO 8601 format                                                                       | 2025-12-19T10:00:00Z                                                                                |
| `to`                                                                                                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)       | :heavy_check_mark:                                                                                  | End time in ISO 8601 format                                                                         | 2025-12-19T11:00:00Z                                                                                |
| `userId`                                                                                            | *string*                                                                                            | :heavy_minus_sign:                                                                                  | User ID to get the graph for (mutually exclusive with external_user_id)                             |                                                                                                     |