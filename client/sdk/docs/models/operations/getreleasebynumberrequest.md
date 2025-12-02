# GetReleaseByNumberRequest

## Example Usage

```typescript
import { GetReleaseByNumberRequest } from "@gram/client/models/operations";

let value: GetReleaseByNumberRequest = {
  toolsetSlug: "<value>",
  releaseNumber: 910198,
};
```

## Fields

| Field                   | Type                    | Required                | Description             |
| ----------------------- | ----------------------- | ----------------------- | ----------------------- |
| `toolsetSlug`           | *string*                | :heavy_check_mark:      | The slug of the toolset |
| `releaseNumber`         | *number*                | :heavy_check_mark:      | The release number      |
| `gramSession`           | *string*                | :heavy_minus_sign:      | Session header          |
| `gramKey`               | *string*                | :heavy_minus_sign:      | API Key header          |
| `gramProject`           | *string*                | :heavy_minus_sign:      | project header          |