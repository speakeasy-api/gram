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

| Field            | Type                                                                                                             | Required           | Description                           |
| ---------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------- |
| `config`         | Record<string, _any_>                                                                                            | :heavy_check_mark: | The trigger config payload.           |
| `definitionSlug` | _string_                                                                                                         | :heavy_check_mark: | The trigger definition slug.          |
| `environmentId`  | _string_                                                                                                         | :heavy_minus_sign: | The linked environment ID.            |
| `name`           | _string_                                                                                                         | :heavy_check_mark: | The trigger instance name.            |
| `status`         | [components.CreateTriggerInstanceFormStatus](../../models/components/createtriggerinstanceformstatus.md)         | :heavy_minus_sign: | Optional initial status.              |
| `targetDisplay`  | _string_                                                                                                         | :heavy_check_mark: | The user-facing target display value. |
| `targetKind`     | [components.CreateTriggerInstanceFormTargetKind](../../models/components/createtriggerinstanceformtargetkind.md) | :heavy_check_mark: | The trigger target kind.              |
| `targetRef`      | _string_                                                                                                         | :heavy_check_mark: | The opaque target reference.          |
