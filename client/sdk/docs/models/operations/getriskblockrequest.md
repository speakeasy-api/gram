# GetRiskBlockRequest

## Example Usage

```typescript
import { GetRiskBlockRequest } from "@gram/client/models/operations/getriskblock.js";

let value: GetRiskBlockRequest = {
  id: "4bf51bf4-7566-4dc7-8659-d8ae6f1ebf00",
};
```

## Fields

| Field         | Type     | Required           | Description                                   |
| ------------- | -------- | ------------------ | --------------------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The block ID (the underlying risk result ID). |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                |
