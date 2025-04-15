# EnvironmentEntry

A single environment entry

## Example Usage

```typescript
import { EnvironmentEntry } from "@gram/client/models/components";

let value: EnvironmentEntry = {
  createdAt: new Date("2024-03-30T11:16:03.610Z"),
  name: "<value>",
  updatedAt: new Date("2023-10-17T22:52:14.955Z"),
  value: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the environment entry                                                    |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the environment variable                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the environment entry was last updated                                                   |
| `value`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | Redacted values of the environment variable                                                   |