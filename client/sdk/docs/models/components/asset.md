# Asset

## Example Usage

```typescript
import { Asset } from "@gram/sdk/models/components";

let value: Asset = {
  contentLength: 5166490435906183000,
  contentType: "Error ad blanditiis asperiores.",
  createdAt: new Date("1984-02-22T22:19:37Z"),
  id: "Non ipsa.",
  kind: "unknown",
  sha256: "Officia est qui labore ut.",
  updatedAt: new Date("1979-11-08T09:19:04Z"),
  url: "Vero id.",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `contentLength`                                                                               | *number*                                                                                      | :heavy_check_mark:                                                                            | The content length of the asset                                                               | 387907201441878175                                                                            |
| `contentType`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The content type of the asset                                                                 | Et corporis reiciendis molestias dolorem.                                                     |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the asset.                                                               | 1993-04-13T19:02:01Z                                                                          |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the asset                                                                           | Quia totam cum excepturi.                                                                     |
| `kind`                                                                                        | [components.Kind](../../models/components/kind.md)                                            | :heavy_check_mark:                                                                            | N/A                                                                                           | openapiv3                                                                                     |
| `sha256`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The SHA256 hash of the asset                                                                  | Quia voluptate mollitia voluptates.                                                           |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the asset.                                                            | 1971-01-06T18:51:36Z                                                                          |
| `url`                                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The URL to the uploaded asset                                                                 | Eius et omnis non reprehenderit quo.                                                          |