# AgentResponseOutput

Response output from an agent workflow

## Example Usage

```typescript
import { AgentResponseOutput } from "@gram/client/models/components";

let value: AgentResponseOutput = {
  createdAt: 791354,
  id: "<id>",
  model: "Roadster",
  object: "<value>",
  output: [
    "<value 1>",
  ],
  result: "<value>",
  status: "in_progress",
  temperature: 9910.01,
  text: {
    format: {
      type: "<value>",
    },
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `createdAt`                                                                  | *number*                                                                     | :heavy_check_mark:                                                           | Unix timestamp when the response was created                                 |
| `error`                                                                      | *string*                                                                     | :heavy_minus_sign:                                                           | Error message if the response failed                                         |
| `id`                                                                         | *string*                                                                     | :heavy_check_mark:                                                           | Unique identifier for this response                                          |
| `instructions`                                                               | *string*                                                                     | :heavy_minus_sign:                                                           | The instructions that were used                                              |
| `model`                                                                      | *string*                                                                     | :heavy_check_mark:                                                           | The model that was used                                                      |
| `object`                                                                     | *string*                                                                     | :heavy_check_mark:                                                           | Object type, always 'response'                                               |
| `output`                                                                     | *any*[]                                                                      | :heavy_check_mark:                                                           | Array of output items (messages, tool calls)                                 |
| `previousResponseId`                                                         | *string*                                                                     | :heavy_minus_sign:                                                           | ID of the previous response if continuing                                    |
| `result`                                                                     | *string*                                                                     | :heavy_check_mark:                                                           | The final text result from the agent                                         |
| `status`                                                                     | [components.Status](../../models/components/status.md)                       | :heavy_check_mark:                                                           | Status of the response                                                       |
| `temperature`                                                                | *number*                                                                     | :heavy_check_mark:                                                           | Temperature that was used                                                    |
| `text`                                                                       | [components.AgentResponseText](../../models/components/agentresponsetext.md) | :heavy_check_mark:                                                           | Text format configuration for the response                                   |