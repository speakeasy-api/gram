# TokenExchangeRequest

## Example Usage

```typescript
import { TokenExchangeRequest } from "@gram/client/models/operations";

let value: TokenExchangeRequest = {
  exchangeRequestBody: {
    email: "dev@acme.corp",
  },
};
```

## Fields

| Field                 | Type                                                                             | Required           | Description    |
| --------------------- | -------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`             | _string_                                                                         | :heavy_minus_sign: | API Key header |
| `exchangeRequestBody` | [components.ExchangeRequestBody](../../models/components/exchangerequestbody.md) | :heavy_check_mark: | N/A            |
