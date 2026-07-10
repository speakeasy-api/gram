# UpdateTriggerInstanceRequest

## Example Usage

```typescript
import { UpdateTriggerInstanceRequest } from "@gram/client/models/operations/updatetriggerinstance.js";

let value: UpdateTriggerInstanceRequest = {
  updateTriggerInstanceForm: {
    id: "6c439b81-4882-4e26-871f-209a03a666d3",
  },
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `updateTriggerInstanceForm`                                                                  | [components.UpdateTriggerInstanceForm](../../models/components/updatetriggerinstanceform.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |