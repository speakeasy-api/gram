# CreateTriggerInstanceForm

## Example Usage

```typescript
import { CreateTriggerInstanceForm } from "@gram/client/models/components/createtriggerinstanceform.js";

let value: CreateTriggerInstanceForm = {
  config: {},
  definitionSlug: "<value>",
  name: "<value>",
  targetDisplay: "<value>",
  targetKind: "assistant",
  targetRef: "<value>",
};
```

## Fields

| Field                                                                                                            | Type                                                                                                             | Required                                                                                                         | Description                                                                                                      |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `config`                                                                                                         | Record<string, *any*>                                                                                            | :heavy_check_mark:                                                                                               | The trigger config payload.                                                                                      |
| `definitionSlug`                                                                                                 | *string*                                                                                                         | :heavy_check_mark:                                                                                               | The trigger definition slug.                                                                                     |
| `environmentId`                                                                                                  | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | The linked environment ID.                                                                                       |
| `name`                                                                                                           | *string*                                                                                                         | :heavy_check_mark:                                                                                               | The trigger instance name.                                                                                       |
| `status`                                                                                                         | [components.CreateTriggerInstanceFormStatus](../../models/components/createtriggerinstanceformstatus.md)         | :heavy_minus_sign:                                                                                               | Optional initial status.                                                                                         |
| `targetDisplay`                                                                                                  | *string*                                                                                                         | :heavy_check_mark:                                                                                               | The user-facing target display value.                                                                            |
| `targetKind`                                                                                                     | [components.CreateTriggerInstanceFormTargetKind](../../models/components/createtriggerinstanceformtargetkind.md) | :heavy_check_mark:                                                                                               | The trigger target kind.                                                                                         |
| `targetRef`                                                                                                      | *string*                                                                                                         | :heavy_check_mark:                                                                                               | The opaque target reference.                                                                                     |