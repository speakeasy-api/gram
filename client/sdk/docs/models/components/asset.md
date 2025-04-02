# Asset

## Example Usage

```typescript
import { Asset } from "@gram/sdk/models/components";

let value: Asset = {
  contentLength: 8318314616491287000,
  contentType: "Totam accusamus.",
  createdAt: new Date("1974-08-26T03:32:17Z"),
  id: "Nihil consequuntur voluptatem sint.",
  kind: "unknown",
  sha256: "Quaerat optio qui quo.",
  updatedAt: new Date("2015-07-15T12:41:58Z"),
  url: "Consectetur velit asperiores temporibus modi.",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `contentLength`                                                                               | *number*                                                                                      | :heavy_check_mark:                                                                            | The content length of the asset                                                               | 164679790983317422                                                                            |
| `contentType`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The content type of the asset                                                                 | Id consequatur illum culpa beatae quos eos.                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the asset.                                                               | 1990-02-27T12:42:47Z                                                                          |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the asset                                                                           | Perspiciatis quae repudiandae voluptatem tenetur totam.                                       |
| `kind`                                                                                        | [components.Kind](../../models/components/kind.md)                                            | :heavy_check_mark:                                                                            | N/A                                                                                           | openapiv3                                                                                     |
| `sha256`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The SHA256 hash of the asset                                                                  | Et voluptatum.                                                                                |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the asset.                                                            | 1982-05-06T03:14:46Z                                                                          |
| `url`                                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The URL to the uploaded asset                                                                 | Sint cupiditate aspernatur nulla nulla aut.                                                   |