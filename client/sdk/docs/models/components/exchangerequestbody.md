# ExchangeRequestBody

## Example Usage

```typescript
import { ExchangeRequestBody } from "@gram/client/models/components";

let value: ExchangeRequestBody = {
  email: "dev@acme.corp",
};
```

## Fields

| Field   | Type     | Required           | Description                                                                                                     | Example       |
| ------- | -------- | ------------------ | --------------------------------------------------------------------------------------------------------------- | ------------- |
| `email` | _string_ | :heavy_check_mark: | Email address of the enrolled user to mint a per-user key for. Resolved to a user within the authenticated org. | dev@acme.corp |
