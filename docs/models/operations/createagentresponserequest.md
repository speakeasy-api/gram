# CreateAgentResponseRequest

## Example Usage

```typescript
import { CreateAgentResponseRequest } from "@gram/client/models/operations";

let value: CreateAgentResponseRequest = {
  agentResponseRequest: {
    input: "<value>",
    model: "Model T",
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `gramKey`                                                                          | *string*                                                                           | :heavy_minus_sign:                                                                 | API Key header                                                                     |
| `gramProject`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | project header                                                                     |
| `agentResponseRequest`                                                             | [components.AgentResponseRequest](../../models/components/agentresponserequest.md) | :heavy_check_mark:                                                                 | N/A                                                                                |