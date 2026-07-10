# ListRiskResultsByChatRequest

## Example Usage

```typescript
import { ListRiskResultsByChatRequest } from "@gram/client/models/operations/listriskresultsbychat.js";

let value: ListRiskResultsByChatRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                   |
| ------------- | -------- | ------------------ | --------------------------------------------- |
| `cursor`      | _string_ | :heavy_minus_sign: | Cursor to fetch the next page of results.     |
| `limit`       | _number_ | :heavy_minus_sign: | Maximum number of results to return per page. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                |
