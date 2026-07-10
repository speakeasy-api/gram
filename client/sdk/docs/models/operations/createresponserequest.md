# CreateResponseRequest

## Example Usage

```typescript
import { CreateResponseRequest } from "@gram/client/models/operations";

let value: CreateResponseRequest = {
  workflowAgentRequest: {
    input: "<value>",
    model: "Roadster",
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `gramKey`                                                                          | *string*                                                                           | :heavy_minus_sign:                                                                 | API Key header                                                                     |
| `gramProject`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | project header                                                                     |
| `workflowAgentRequest`                                                             | [components.WorkflowAgentRequest](../../models/components/workflowagentrequest.md) | :heavy_check_mark:                                                                 | N/A                                                                                |