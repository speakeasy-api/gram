# ListAssetsResult

## Example Usage

```typescript
import { ListAssetsResult } from "@gram/client/models/components";

let value: ListAssetsResult = {
  assets: [
    {
      contentLength: 346222,
      contentType: "<value>",
      createdAt: new Date("2023-07-27T01:38:28.246Z"),
      id: "<id>",
      kind: "functions",
      sha256: "<value>",
      updatedAt: new Date("2023-02-10T06:07:24.864Z"),
    },
  ],
};
```

## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `assets`                                               | [components.Asset](../../models/components/asset.md)[] | :heavy_check_mark:                                     | The list of assets                                     |