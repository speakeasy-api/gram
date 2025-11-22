# DeleteToolsetEnvironmentLinkRequest

## Example Usage

```typescript
import { DeleteToolsetEnvironmentLinkRequest } from "@gram/client/models/operations";

let value: DeleteToolsetEnvironmentLinkRequest = {
  toolsetId: "dd45a394-b5a5-4563-8dcf-3f1494962376",
};
```

## Fields

| Field                 | Type                  | Required              | Description           |
| --------------------- | --------------------- | --------------------- | --------------------- |
| `toolsetId`           | *string*              | :heavy_check_mark:    | The ID of the toolset |
| `gramSession`         | *string*              | :heavy_minus_sign:    | Session header        |
| `gramProject`         | *string*              | :heavy_minus_sign:    | project header        |