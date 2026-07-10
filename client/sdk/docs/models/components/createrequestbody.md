# CreateRequestBody

## Example Usage

```typescript
import { CreateRequestBody } from "@gram/client/models/components/createrequestbody.js";

let value: CreateRequestBody = {
  embedOrigin: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                      |
| ---------------- | -------- | ------------------ | ------------------------------------------------ |
| `embedOrigin`    | _string_ | :heavy_check_mark: | The origin from which the token will be used     |
| `expiresAfter`   | _number_ | :heavy_minus_sign: | Token expiration in seconds (max / default 3600) |
| `userIdentifier` | _string_ | :heavy_minus_sign: | Optional free-form user identifier               |
