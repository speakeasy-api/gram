# DeleteRiskPolicyRequest

## Example Usage

```typescript
import { DeleteRiskPolicyRequest } from "@gram/client/models/operations/deleteriskpolicy.js";

let value: DeleteRiskPolicyRequest = {
  id: "07bea438-70a0-4361-8424-78016a58a55c",
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `id`               | *string*           | :heavy_check_mark: | The policy ID.     |
| `gramKey`          | *string*           | :heavy_minus_sign: | API Key header     |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |