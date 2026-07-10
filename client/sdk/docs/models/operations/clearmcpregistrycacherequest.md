# ClearMCPRegistryCacheRequest

## Example Usage

```typescript
import { ClearMCPRegistryCacheRequest } from "@gram/client/models/operations/clearmcpregistrycache.js";

let value: ClearMCPRegistryCacheRequest = {
  registryId: "675b0149-7f94-4bbc-a042-2869706efea3",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `registryId`                    | *string*                        | :heavy_check_mark:              | The registry to clear cache for |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |
| `gramKey`                       | *string*                        | :heavy_minus_sign:              | API Key header                  |
| `gramProject`                   | *string*                        | :heavy_minus_sign:              | project header                  |