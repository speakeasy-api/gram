# ListCollectionServersRequest

## Example Usage

```typescript
import { ListCollectionServersRequest } from "@gram/client/models/operations/listcollectionservers.js";

let value: ListCollectionServersRequest = {
  collectionSlug: "<value>",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `collectionSlug`                | *string*                        | :heavy_check_mark:              | Slug of the collection to serve |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |
| `gramKey`                       | *string*                        | :heavy_minus_sign:              | API Key header                  |