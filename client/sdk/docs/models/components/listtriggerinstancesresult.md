# ListTriggerInstancesResult

## Example Usage

```typescript
import { ListTriggerInstancesResult } from "@gram/client/models/components/listtriggerinstancesresult.js";

let value: ListTriggerInstancesResult = {
  triggers: [],
};
```

## Fields

| Field      | Type                                                                       | Required           | Description                                    |
| ---------- | -------------------------------------------------------------------------- | ------------------ | ---------------------------------------------- |
| `triggers` | [components.TriggerInstance](../../models/components/triggerinstance.md)[] | :heavy_check_mark: | The trigger instances for the current project. |
