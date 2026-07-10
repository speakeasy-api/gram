# GetAssistantRequest

## Example Usage

```typescript
import { GetAssistantRequest } from "@gram/client/models/operations/getassistant.js";

let value: GetAssistantRequest = {
  id: "4e7a371e-6790-42a7-b5dd-f5743b144784",
};
```

## Fields

| Field         | Type     | Required           | Description       |
| ------------- | -------- | ------------------ | ----------------- |
| `id`          | _string_ | :heavy_check_mark: | The assistant ID. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header    |
| `gramProject` | _string_ | :heavy_minus_sign: | project header    |
