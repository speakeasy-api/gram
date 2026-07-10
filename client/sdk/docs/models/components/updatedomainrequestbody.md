# UpdateDomainRequestBody

## Example Usage

```typescript
import { UpdateDomainRequestBody } from "@gram/client/models/components/updatedomainrequestbody.js";

let value: UpdateDomainRequestBody = {
  ipAllowlist: [
    "<value 1>",
    "<value 2>",
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `ipAllowlist`                                                            | *string*[]                                                               | :heavy_check_mark:                                                       | Replacement IP allowlist. Pass an empty list to remove all restrictions. |