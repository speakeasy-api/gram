# ListAssetsResult

## Example Usage

```typescript
import { ListAssetsResult } from "@gram/client/models/components";

let value: ListAssetsResult = {
  assets: [
    {
      contentLength: 346222,
      contentType: "<value>",
      createdAt: new Date("2024-07-26T01:38:28.246Z"),
      id: "<id>",
      kind: "chat_attachment",
      sha256: "<value>",
      updatedAt: new Date("2024-02-10T06:07:24.864Z"),
    },
  ],
};
```

## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `assets`                                               | [components.Asset](../../models/components/asset.md)[] | :heavy_check_mark:                                     | The list of assets                                     |