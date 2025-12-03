# AgentSubAgent

A sub-agent definition for the agent workflow

## Example Usage

```typescript
import { AgentSubAgent } from "@gram/client/models/components";

let value: AgentSubAgent = {
  description: "inferior hastily phooey acceptable",
  name: "<value>",
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `description`                                                        | *string*                                                             | :heavy_check_mark:                                                   | Description of what this sub-agent does                              |
| `environmentSlug`                                                    | *string*                                                             | :heavy_minus_sign:                                                   | The environment slug for auth                                        |
| `instructions`                                                       | *string*                                                             | :heavy_minus_sign:                                                   | Instructions for this sub-agent                                      |
| `name`                                                               | *string*                                                             | :heavy_check_mark:                                                   | The name of this sub-agent                                           |
| `tools`                                                              | *string*[]                                                           | :heavy_minus_sign:                                                   | Tool URNs available to this sub-agent                                |
| `toolsets`                                                           | [components.AgentToolset](../../models/components/agenttoolset.md)[] | :heavy_minus_sign:                                                   | Toolsets available to this sub-agent                                 |