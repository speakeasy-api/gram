# EmployeeDataFlowNode

A node in the employee data flow graph

## Example Usage

```typescript
import { EmployeeDataFlowNode } from "@gram/client/models/components/employeedataflownode.js";

let value: EmployeeDataFlowNode = {
  id: "<id>",
  label: "<value>",
  tier: "server",
  totalCalls: 272862,
};
```

## Fields

| Field                                                                                                           | Type                                                                                                            | Required                                                                                                        | Description                                                                                                     |
| --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                            | *string*                                                                                                        | :heavy_check_mark:                                                                                              | Stable node ID                                                                                                  |
| `label`                                                                                                         | *string*                                                                                                        | :heavy_check_mark:                                                                                              | Display label                                                                                                   |
| `serverClass`                                                                                                   | [components.ServerClass](../../models/components/serverclass.md)                                                | :heavy_minus_sign:                                                                                              | Server classification, present for MCP server nodes                                                             |
| `tier`                                                                                                          | [components.Tier](../../models/components/tier.md)                                                              | :heavy_check_mark:                                                                                              | Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL. |
| `totalCalls`                                                                                                    | *number*                                                                                                        | :heavy_check_mark:                                                                                              | Total calls involving this node                                                                                 |