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

| Field           | Type                                                                 | Required           | Description    |
| --------------- | -------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`   | _string_                                                             | :heavy_minus_sign: | Session header |
| `gramKey`       | _string_                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`   | _string_                                                             | :heavy_minus_sign: | project header |
| `verifyURLForm` | [components.VerifyURLForm](../../models/components/verifyurlform.md) | :heavy_check_mark: | N/A            |
