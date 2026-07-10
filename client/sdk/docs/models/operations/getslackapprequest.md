# GetSlackAppRequest

## Example Usage

```typescript
import { GetSlackAppRequest } from "@gram/client/models/operations";

let value: GetSlackAppRequest = {
  id: "04ccca35-0f51-4c3f-8708-e17c8f5e59a6",
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `id`               | *string*           | :heavy_check_mark: | The Slack app ID   |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |