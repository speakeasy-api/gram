# CreateTriggerInstanceRequest

## Example Usage

```typescript
import { CreateTriggerInstanceRequest } from "@gram/client/models/operations/createtriggerinstance.js";

let value: CreateTriggerInstanceRequest = {
  createTriggerInstanceForm: {
    config: {
      "key": "<value>",
      "key1": "<value>",
    },
    definitionSlug: "<value>",
    name: "<value>",
    targetDisplay: "<value>",
    targetKind: "noop",
    targetRef: "<value>",
  },
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `createTriggerInstanceForm`                                                                  | [components.CreateTriggerInstanceForm](../../models/components/createtriggerinstanceform.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |