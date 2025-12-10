# GetInstanceRequest

## Example Usage

```typescript
import { GetInstanceRequest } from "@gram/client/models/operations";

let value: GetInstanceRequest = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `toolsetSlug`                   | *string*                        | :heavy_check_mark:              | The slug of the toolset to load |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |
| `gramProject`                   | *string*                        | :heavy_minus_sign:              | project header                  |
| `gramKey`                       | *string*                        | :heavy_minus_sign:              | API Key header                  |