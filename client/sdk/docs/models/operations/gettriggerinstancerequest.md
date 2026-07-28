# GetTriggerInstanceRequest

## Example Usage

```typescript
import { GetTriggerInstanceRequest } from "@gram/client/models/operations/gettriggerinstance.js";

let value: GetTriggerInstanceRequest = {
  id: "eb6e01eb-afed-4206-a01f-25f7b9e8871e",
};
```

## Fields

| Field         | Type     | Required           | Description              |
| ------------- | -------- | ------------------ | ------------------------ |
| `id`          | _string_ | :heavy_check_mark: | The trigger instance ID. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header           |
| `gramProject` | _string_ | :heavy_minus_sign: | project header           |
