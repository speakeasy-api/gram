# MoveIssuerRequestBody

## Example Usage

```typescript
import { MoveIssuerRequestBody } from "@gram/client/models/components/moveissuerrequestbody.js";

let value: MoveIssuerRequestBody = {
  id: "d44f62f6-f728-4354-8ef1-17e1b316edfe",
};
```

## Fields

| Field                                                                                                                       | Type                                                                                                                        | Required                                                                                                                    | Description                                                                                                                 |
| --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                                        | *string*                                                                                                                    | :heavy_check_mark:                                                                                                          | The remote_session_issuer id.                                                                                               |
| `projectId`                                                                                                                 | *string*                                                                                                                    | :heavy_minus_sign:                                                                                                          | Target owning project id; the project must belong to the caller's organization. Omit to make the issuer organization-level. |