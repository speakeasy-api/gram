# VerifyRemoteMcpURLRequest

## Example Usage

```typescript
import { VerifyRemoteMcpURLRequest } from "@gram/client/models/operations/verifyremotemcpurl.js";

let value: VerifyRemoteMcpURLRequest = {
  verifyURLForm: {
    transportType: "<value>",
    url: "https://quarterly-giggle.org",
  },
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `gramSession`                                                        | *string*                                                             | :heavy_minus_sign:                                                   | Session header                                                       |
| `gramKey`                                                            | *string*                                                             | :heavy_minus_sign:                                                   | API Key header                                                       |
| `gramProject`                                                        | *string*                                                             | :heavy_minus_sign:                                                   | project header                                                       |
| `verifyURLForm`                                                      | [components.VerifyURLForm](../../models/components/verifyurlform.md) | :heavy_check_mark:                                                   | N/A                                                                  |