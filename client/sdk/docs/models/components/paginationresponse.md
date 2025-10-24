# PaginationResponse

Pagination metadata for list responses

## Example Usage

```typescript
import { PaginationResponse } from "@gram/client/models/components";

let value: PaginationResponse = {};
```

## Fields

| Field                        | Type                         | Required                     | Description                  |
| ---------------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| `hasNextPage`                | *boolean*                    | :heavy_minus_sign:           | Whether there is a next page |
| `nextPageCursor`             | *string*                     | :heavy_minus_sign:           | Cursor for next page         |
| `perPage`                    | *number*                     | :heavy_minus_sign:           | Number of items per page     |