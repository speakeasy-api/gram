# CreateDomainRequestBody

## Example Usage

```typescript
import { CreateDomainRequestBody } from "@gram/client/models/components/createdomainrequestbody.js";

let value: CreateDomainRequestBody = {
  domain: "definite-technologist.info",
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `domain`                                                                   | *string*                                                                   | :heavy_check_mark:                                                         | The custom domain                                                          |
| `ipAllowlist`                                                              | *string*[]                                                                 | :heavy_minus_sign:                                                         | IP addresses or CIDR ranges to allow. Leave empty for unrestricted access. |