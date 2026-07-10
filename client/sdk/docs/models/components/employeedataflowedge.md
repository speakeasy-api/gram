# EmployeeDataFlowEdge

A weighted edge in the employee data flow graph

## Example Usage

```typescript
import { EmployeeDataFlowEdge } from "@gram/client/models/components/employeedataflowedge.js";

let value: EmployeeDataFlowEdge = {
  callCount: 125848,
  failureCount: 223832,
  id: "<id>",
  source: "<value>",
  successCount: 119508,
  target: "<value>",
};
```

## Fields

| Field                                            | Type                                             | Required                                         | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ |
| `callCount`                                      | *number*                                         | :heavy_check_mark:                               | Total calls represented by this edge             |
| `failureCount`                                   | *number*                                         | :heavy_check_mark:                               | Failed or blocked calls represented by this edge |
| `id`                                             | *string*                                         | :heavy_check_mark:                               | Stable edge ID                                   |
| `source`                                         | *string*                                         | :heavy_check_mark:                               | Source node ID                                   |
| `successCount`                                   | *number*                                         | :heavy_check_mark:                               | Successful calls represented by this edge        |
| `target`                                         | *string*                                         | :heavy_check_mark:                               | Target node ID                                   |