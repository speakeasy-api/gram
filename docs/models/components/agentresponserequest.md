# AgentResponseRequest

Request payload for creating an agent response

## Example Usage

```typescript
import { AgentResponseRequest } from "@gram/client/models/components";

let value: AgentResponseRequest = {
  input: "<value>",
  model: "Accord",
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `async`                                                                | *boolean*                                                              | :heavy_minus_sign:                                                     | If true, returns immediately with a response ID for polling            |
| `input`                                                                | *any*                                                                  | :heavy_check_mark:                                                     | The input to the agent - can be a string or array of messages          |
| `instructions`                                                         | *string*                                                               | :heavy_minus_sign:                                                     | System instructions for the agent                                      |
| `model`                                                                | *string*                                                               | :heavy_check_mark:                                                     | The model to use for the agent (e.g., openai/gpt-4o)                   |
| `previousResponseId`                                                   | *string*                                                               | :heavy_minus_sign:                                                     | ID of a previous response to continue from                             |
| `store`                                                                | *boolean*                                                              | :heavy_minus_sign:                                                     | If true, stores the response defaults to true                          |
| `subAgents`                                                            | [components.AgentSubAgent](../../models/components/agentsubagent.md)[] | :heavy_minus_sign:                                                     | Sub-agents available for delegation                                    |
| `temperature`                                                          | *number*                                                               | :heavy_minus_sign:                                                     | Temperature for model responses                                        |
| `toolsets`                                                             | [components.AgentToolset](../../models/components/agenttoolset.md)[]   | :heavy_minus_sign:                                                     | Toolsets available to the agent                                        |