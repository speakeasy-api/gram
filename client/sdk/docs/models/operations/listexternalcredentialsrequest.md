# ListExternalCredentialsRequest

## Example Usage

```typescript
import { ListExternalCredentialsRequest } from "@gram/client/models/operations/listexternalcredentials.js";

let value: ListExternalCredentialsRequest = {};
```

## Fields

| Field         | Type                                                       | Required           | Description                                |
| ------------- | ---------------------------------------------------------- | ------------------ | ------------------------------------------ |
| `provider`    | [operations.Provider](../../models/operations/provider.md) | :heavy_minus_sign: | Only return credentials for this provider. |
| `gramSession` | _string_                                                   | :heavy_minus_sign: | Session header                             |
