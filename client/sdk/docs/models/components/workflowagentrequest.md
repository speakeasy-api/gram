# WorkflowAgentRequest

Request payload for creating an agent response

## Example Usage

```typescript
import { WorkflowAgentRequest } from "@gram/client/models/components";

let value: WorkflowAgentRequest = {
  input: "<value>",
  model: "Roadster",
};
```

## Fields

| Field                | Type                                                                                 | Required           | Description                                                   |
| -------------------- | ------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------- |
| `async`              | _boolean_                                                                            | :heavy_minus_sign: | If true, returns immediately with a response ID for polling   |
| `input`              | _any_                                                                                | :heavy_check_mark: | The input to the agent - can be a string or array of messages |
| `instructions`       | _string_                                                                             | :heavy_minus_sign: | System instructions for the agent                             |
| `model`              | _string_                                                                             | :heavy_check_mark: | The model to use for the agent (e.g., openai/gpt-4o)          |
| `previousResponseId` | _string_                                                                             | :heavy_minus_sign: | ID of a previous response to continue from                    |
| `store`              | _boolean_                                                                            | :heavy_minus_sign: | If true, stores the response defaults to true                 |
| `subAgents`          | [components.WorkflowSubAgent](../../models/components/workflowsubagent.md)[]         | :heavy_minus_sign: | Sub-agents available for delegation                           |
| `temperature`        | _number_                                                                             | :heavy_minus_sign: | Temperature for model responses                               |
| `toolsets`           | [components.WorkflowAgentToolset](../../models/components/workflowagenttoolset.md)[] | :heavy_minus_sign: | Toolsets available to the agent                               |
