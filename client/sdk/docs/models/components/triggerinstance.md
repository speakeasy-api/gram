# TriggerInstance

## Example Usage

```typescript
import { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";

let value: TriggerInstance = {
  config: {
    "key": "<value>",
    "key1": "<value>",
  },
  createdAt: new Date("2026-09-18T05:24:46.807Z"),
  definitionSlug: "<value>",
  id: "02d8d7c1-48a7-4e74-a542-62b3ff34524f",
  name: "<value>",
  projectId: "aaafbcad-f297-474f-9829-604225d4afb2",
  status: "cancelled",
  targetDisplay: "<value>",
  targetKind: "<value>",
  targetRef: "<value>",
  updatedAt: new Date("2026-11-15T12:47:18.461Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `config`                                                                                      | Record<string, *any*>                                                                         | :heavy_check_mark:                                                                            | The trigger config payload.                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Creation timestamp.                                                                           |
| `definitionSlug`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The trigger definition slug.                                                                  |
| `environmentId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The linked environment ID.                                                                    |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The trigger instance ID.                                                                      |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The trigger instance name.                                                                    |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID owning the trigger instance.                                                   |
| `status`                                                                                      | [components.TriggerInstanceStatus](../../models/components/triggerinstancestatus.md)          | :heavy_check_mark:                                                                            | The trigger instance status.                                                                  |
| `targetDisplay`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The user-facing target display value.                                                         |
| `targetKind`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The target kind for the trigger instance.                                                     |
| `targetRef`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The opaque target reference.                                                                  |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Last update timestamp.                                                                        |
| `webhookUrl`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Webhook URL for webhook-backed triggers.                                                      |