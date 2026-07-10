# WorkflowAgentResponseOutput

Response output from an agent workflow

## Example Usage

```typescript
import { WorkflowAgentResponseOutput } from "@gram/client/models/components";

let value: WorkflowAgentResponseOutput = {
  createdAt: 91271,
  id: "<id>",
  model: "ATS",
  object: "<value>",
  output: ["<value 1>", "<value 2>"],
  result: "<value>",
  status: "failed",
  temperature: 1157.22,
  text: {
    format: {
      type: "<value>",
    },
  },
};
```

## Fields

| Field                | Type                                                                                                         | Required           | Description                                  |
| -------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | -------------------------------------------- |
| `createdAt`          | _number_                                                                                                     | :heavy_check_mark: | Unix timestamp when the response was created |
| `error`              | _string_                                                                                                     | :heavy_minus_sign: | Error message if the response failed         |
| `id`                 | _string_                                                                                                     | :heavy_check_mark: | Unique identifier for this response          |
| `instructions`       | _string_                                                                                                     | :heavy_minus_sign: | The instructions that were used              |
| `model`              | _string_                                                                                                     | :heavy_check_mark: | The model that was used                      |
| `object`             | _string_                                                                                                     | :heavy_check_mark: | Object type, always 'response'               |
| `output`             | _any_[]                                                                                                      | :heavy_check_mark: | Array of output items (messages, tool calls) |
| `previousResponseId` | _string_                                                                                                     | :heavy_minus_sign: | ID of the previous response if continuing    |
| `result`             | _string_                                                                                                     | :heavy_check_mark: | The final text result from the agent         |
| `status`             | [components.WorkflowAgentResponseOutputStatus](../../models/components/workflowagentresponseoutputstatus.md) | :heavy_check_mark: | Status of the response                       |
| `temperature`        | _number_                                                                                                     | :heavy_check_mark: | Temperature that was used                    |
| `text`               | [components.WorkflowAgentResponseText](../../models/components/workflowagentresponsetext.md)                 | :heavy_check_mark: | Text format configuration for the response   |
