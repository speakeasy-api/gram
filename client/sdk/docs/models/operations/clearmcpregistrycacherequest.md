# ClearMCPRegistryCacheRequest

## Example Usage

```typescript
import { ClearMCPRegistryCacheRequest } from "@gram/client/models/operations/clearmcpregistrycache.js";

let value: ClearMCPRegistryCacheRequest = {
  registryId: "675b0149-7f94-4bbc-a042-2869706efea3",
};
```

## Fields

| Field         | Type     | Required           | Description                     |
| ------------- | -------- | ------------------ | ------------------------------- |
| `registryId`  | _string_ | :heavy_check_mark: | The registry to clear cache for |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                  |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                  |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                  |
