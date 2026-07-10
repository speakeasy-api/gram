# WorkflowSubAgent

A sub-agent definition for the agent workflow

## Example Usage

```typescript
import { WorkflowSubAgent } from "@gram/client/models/components";

let value: WorkflowSubAgent = {
  description:
    "breakable exasperation limited first gadzooks because kookily apud behind whereas",
  name: "<value>",
};
```

## Fields

| Field             | Type                                                                                 | Required           | Description                             |
| ----------------- | ------------------------------------------------------------------------------------ | ------------------ | --------------------------------------- |
| `description`     | _string_                                                                             | :heavy_check_mark: | Description of what this sub-agent does |
| `environmentSlug` | _string_                                                                             | :heavy_minus_sign: | The environment slug for auth           |
| `instructions`    | _string_                                                                             | :heavy_minus_sign: | Instructions for this sub-agent         |
| `name`            | _string_                                                                             | :heavy_check_mark: | The name of this sub-agent              |
| `tools`           | _string_[]                                                                           | :heavy_minus_sign: | Tool URNs available to this sub-agent   |
| `toolsets`        | [components.WorkflowAgentToolset](../../models/components/workflowagenttoolset.md)[] | :heavy_minus_sign: | Toolsets available to this sub-agent    |
