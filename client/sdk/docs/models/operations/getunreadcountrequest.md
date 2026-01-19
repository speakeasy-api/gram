# GetUnreadCountRequest

## Example Usage

```typescript
import { GetUnreadCountRequest } from "@gram/client/models/operations";

let value: GetUnreadCountRequest = {};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `since`                                                                                       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | ISO timestamp to count notifications from                                                     |
| `gramSession`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Session header                                                                                |
| `gramProject`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | project header                                                                                |