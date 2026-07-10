# GetRiskPolicyStatusRequest

## Example Usage

```typescript
import { GetRiskPolicyStatusRequest } from "@gram/client/models/operations/getriskpolicystatus.js";

let value: GetRiskPolicyStatusRequest = {
  id: "2cc09be1-7adb-49b1-994d-2b0931657a29",
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `id`               | *string*           | :heavy_check_mark: | The policy ID.     |
| `gramKey`          | *string*           | :heavy_minus_sign: | API Key header     |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |