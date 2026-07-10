# MintUserSessionRequest

## Example Usage

```typescript
import { MintUserSessionRequest } from "@gram/client/models/operations/mintusersession.js";

let value: MintUserSessionRequest = {
  mintUserSessionRequestBody: {},
};
```

## Fields

| Field                        | Type                                                                                           | Required           | Description    |
| ---------------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                | _string_                                                                                       | :heavy_minus_sign: | Session header |
| `gramProject`                | _string_                                                                                       | :heavy_minus_sign: | project header |
| `mintUserSessionRequestBody` | [components.MintUserSessionRequestBody](../../models/components/mintusersessionrequestbody.md) | :heavy_check_mark: | N/A            |
