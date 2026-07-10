# DeleteSlackAppRequest

## Example Usage

```typescript
import { DeleteSlackAppRequest } from "@gram/client/models/operations";

let value: DeleteSlackAppRequest = {
  id: "bfd74dd2-19b5-4b0f-ae77-9d2ce34871ff",
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `id`               | *string*           | :heavy_check_mark: | The Slack app ID   |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |