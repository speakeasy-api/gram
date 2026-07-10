# GetEmployeeDataFlowGraphResult

Result of employee data flow graph query

## Example Usage

```typescript
import { GetEmployeeDataFlowGraphResult } from "@gram/client/models/components/getemployeedataflowgraphresult.js";

let value: GetEmployeeDataFlowGraphResult = {
  edges: [
    {
      callCount: 8550,
      failureCount: 198685,
      id: "<id>",
      source: "<value>",
      successCount: 602518,
      target: "<value>",
    },
  ],
  nodes: [],
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `edges`                                                                              | [components.EmployeeDataFlowEdge](../../models/components/employeedataflowedge.md)[] | :heavy_check_mark:                                                                   | Weighted graph edges between adjacent populated tiers                                |
| `nodes`                                                                              | [components.EmployeeDataFlowNode](../../models/components/employeedataflownode.md)[] | :heavy_check_mark:                                                                   | Graph nodes grouped by tier                                                          |