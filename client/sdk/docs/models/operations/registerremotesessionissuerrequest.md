# RegisterRemoteSessionIssuerRequest

## Example Usage

```typescript
import { RegisterRemoteSessionIssuerRequest } from "@gram/client/models/operations";

let value: RegisterRemoteSessionIssuerRequest = {
  registerRemoteSessionIssuerForm: {
    remoteSessionIssuerId: "75356154-934b-4da8-8ab1-82c5f4aa0131",
    userSessionIssuerId: "6f22d61b-5c3f-401b-ab1d-1994181877b1",
  },
};
```

## Fields

| Field                             | Type                                                                                                     | Required           | Description    |
| --------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                     | _string_                                                                                                 | :heavy_minus_sign: | Session header |
| `gramKey`                         | _string_                                                                                                 | :heavy_minus_sign: | API Key header |
| `gramProject`                     | _string_                                                                                                 | :heavy_minus_sign: | project header |
| `registerRemoteSessionIssuerForm` | [components.RegisterRemoteSessionIssuerForm](../../models/components/registerremotesessionissuerform.md) | :heavy_check_mark: | N/A            |
