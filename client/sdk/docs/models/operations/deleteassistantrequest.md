# DeleteAssistantRequest

## Example Usage

```typescript
import { DeleteAssistantRequest } from "@gram/client/models/operations/deleteassistant.js";

let value: DeleteAssistantRequest = {
  id: "91a58b88-9f24-4c2f-8f81-5bc1c9d491f3",
};
```

## Fields

| Field         | Type     | Required           | Description       |
| ------------- | -------- | ------------------ | ----------------- |
| `id`          | _string_ | :heavy_check_mark: | The assistant ID. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header    |
| `gramProject` | _string_ | :heavy_minus_sign: | project header    |
