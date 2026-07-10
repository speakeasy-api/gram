# ListTriggerDefinitionsResult

## Example Usage

```typescript
import { ListTriggerDefinitionsResult } from "@gram/client/models/components/listtriggerdefinitionsresult.js";

let value: ListTriggerDefinitionsResult = {
  definitions: [
    {
      configSchema: "{key: 1625880966672995, key1: null, key2: \"<value>\"}",
      description:
        "blah reproachfully before closely nervously than outlying plus excepting",
      envRequirements: [],
      kind: "schedule",
      slug: "<value>",
      title: "<value>",
    },
  ],
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `definitions`                                                                  | [components.TriggerDefinition](../../models/components/triggerdefinition.md)[] | :heavy_check_mark:                                                             | The available trigger definitions.                                             |