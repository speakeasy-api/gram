# ListSlackAppsResult

## Example Usage

```typescript
import { ListSlackAppsResult } from "@gram/client/models/components";

let value: ListSlackAppsResult = {
  items: [
    {
      createdAt: new Date("2024-10-13T08:18:11.018Z"),
      id: "51091588-0481-482b-979d-e52e17a6374e",
      name: "<value>",
      status: "<value>",
      toolsetIds: [
        "<value 1>",
        "<value 2>",
        "<value 3>",
      ],
      updatedAt: new Date("2026-05-13T17:45:02.449Z"),
    },
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `items`                                                                  | [components.SlackAppResult](../../models/components/slackappresult.md)[] | :heavy_check_mark:                                                       | List of Slack apps                                                       |