# ServeFunctionRequest

## Example Usage

```typescript
import { ServeFunctionRequest } from "@gram/client/models/operations/servefunction.js";

let value: ServeFunctionRequest = {
  id: "<id>",
  projectId: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                              |
| ------------- | -------- | ------------------ | ---------------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the asset to serve             |
| `projectId`   | _string_ | :heavy_check_mark: | The procect ID that the asset belongs to |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                           |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                           |
