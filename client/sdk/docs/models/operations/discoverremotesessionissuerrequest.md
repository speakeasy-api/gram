# DiscoverRemoteSessionIssuerRequest

## Example Usage

```typescript
import { DiscoverRemoteSessionIssuerRequest } from "@gram/client/models/operations/discoverremotesessionissuer.js";

let value: DiscoverRemoteSessionIssuerRequest = {
  discoverRemoteSessionIssuerRequestBody: {
    issuer: "mastercard",
  },
};
```

## Fields

| Field                                    | Type                                                                                                                   | Required           | Description    |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                            | _string_                                                                                                               | :heavy_minus_sign: | Session header |
| `gramKey`                                | _string_                                                                                                               | :heavy_minus_sign: | API Key header |
| `gramProject`                            | _string_                                                                                                               | :heavy_minus_sign: | project header |
| `discoverRemoteSessionIssuerRequestBody` | [components.DiscoverRemoteSessionIssuerRequestBody](../../models/components/discoverremotesessionissuerrequestbody.md) | :heavy_check_mark: | N/A            |
