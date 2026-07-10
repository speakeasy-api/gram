# SetToolsetUserSessionIssuerRequest

## Example Usage

```typescript
import { SetToolsetUserSessionIssuerRequest } from "@gram/client/models/operations/settoolsetusersessionissuer.js";

let value: SetToolsetUserSessionIssuerRequest = {
  slug: "<value>",
  setUserSessionIssuerRequestBody: {},
};
```

## Fields

| Field                             | Type                                                                                                     | Required           | Description                     |
| --------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------- |
| `slug`                            | _string_                                                                                                 | :heavy_check_mark: | The slug of the toolset to link |
| `gramSession`                     | _string_                                                                                                 | :heavy_minus_sign: | Session header                  |
| `gramKey`                         | _string_                                                                                                 | :heavy_minus_sign: | API Key header                  |
| `gramProject`                     | _string_                                                                                                 | :heavy_minus_sign: | project header                  |
| `setUserSessionIssuerRequestBody` | [components.SetUserSessionIssuerRequestBody](../../models/components/setusersessionissuerrequestbody.md) | :heavy_check_mark: | N/A                             |
