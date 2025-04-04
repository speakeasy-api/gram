# EnvironmentEntry

A single environment entry

## Example Usage

```typescript
import { EnvironmentEntry } from "@gram/sdk/models/components";

let value: EnvironmentEntry = {
  createdAt: new Date("2024-07-25T22:41:53.719Z"),
  name: "<value>",
  updatedAt: new Date("2024-03-30T11:16:03.610Z"),
  value: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the environment entry                                                    |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the environment variable                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the environment entry was last updated                                                   |
| `value`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The value of the environment variable                                                         |