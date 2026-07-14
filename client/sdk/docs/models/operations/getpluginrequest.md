# GetPluginRequest

## Example Usage

```typescript
import { GetPluginRequest } from "@gram/client/models/operations/getplugin.js";

let value: GetPluginRequest = {
  id: "8cfb242c-aa8e-416c-bcb4-c2886ec10af3",
};
```

## Fields

| Field         | Type     | Required           | Description    |
| ------------- | -------- | ------------------ | -------------- |
| `id`          | _string_ | :heavy_check_mark: | N/A            |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header |
| `gramProject` | _string_ | :heavy_minus_sign: | project header |
