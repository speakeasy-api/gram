# ListCollectionServersRequest

## Example Usage

```typescript
import { ListCollectionServersRequest } from "@gram/client/models/operations/listcollectionservers.js";

let value: ListCollectionServersRequest = {
  collectionSlug: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                     |
| ---------------- | -------- | ------------------ | ------------------------------- |
| `collectionSlug` | _string_ | :heavy_check_mark: | Slug of the collection to serve |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header                  |
| `gramKey`        | _string_ | :heavy_minus_sign: | API Key header                  |
