# WorkflowAgentToolset

A toolset reference for agent execution

## Example Usage

```typescript
import { WorkflowAgentToolset } from "@gram/client/models/components";

let value: WorkflowAgentToolset = {
  environmentSlug: "<value>",
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `environmentSlug`                    | *string*                             | :heavy_check_mark:                   | The slug of the environment for auth |
| `toolsetSlug`                        | *string*                             | :heavy_check_mark:                   | The slug of the toolset to use       |