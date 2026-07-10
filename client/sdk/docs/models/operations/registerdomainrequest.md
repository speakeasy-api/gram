# RegisterDomainRequest

## Example Usage

```typescript
import { RegisterDomainRequest } from "@gram/client/models/operations/registerdomain.js";

let value: RegisterDomainRequest = {
  createDomainRequestBody: {
    domain: "salty-patroller.info",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `createDomainRequestBody`                                                                | [components.CreateDomainRequestBody](../../models/components/createdomainrequestbody.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |