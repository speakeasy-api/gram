# ListReleasesResult

## Example Usage

```typescript
import { ListReleasesResult } from "@gram/client/models/components";

let value: ListReleasesResult = {
  releases: [],
  total: 869760,
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `releases`                                                               | [components.ToolsetRelease](../../models/components/toolsetrelease.md)[] | :heavy_check_mark:                                                       | List of releases                                                         |
| `total`                                                                  | *number*                                                                 | :heavy_check_mark:                                                       | Total number of releases                                                 |