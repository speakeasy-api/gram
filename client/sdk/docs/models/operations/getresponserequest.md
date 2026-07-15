# GetResponseRequest

## Example Usage

```typescript
import { GetResponseRequest } from "@gram/client/models/operations";

let value: GetResponseRequest = {
  responseId: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                        |
| ------------- | -------- | ------------------ | ---------------------------------- |
| `responseId`  | _string_ | :heavy_check_mark: | The ID of the response to retrieve |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                     |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                     |
