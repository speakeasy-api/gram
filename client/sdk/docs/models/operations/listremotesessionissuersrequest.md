# ListRemoteSessionIssuersRequest

## Example Usage

```typescript
import { ListRemoteSessionIssuersRequest } from "@gram/client/models/operations/listremotesessionissuers.js";

let value: ListRemoteSessionIssuersRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                      |
| ------------- | -------- | ------------------ | -------------------------------- |
| `cursor`      | _string_ | :heavy_minus_sign: | Pagination cursor.               |
| `limit`       | _number_ | :heavy_minus_sign: | Page size (default 50, max 100). |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                   |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                   |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                   |
