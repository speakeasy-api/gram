# CreditUsageResponseBody

## Example Usage

```typescript
import { CreditUsageResponseBody } from "@gram/client/models/components";

let value: CreditUsageResponseBody = {
  creditsUsed: 4976.91,
  monthlyCredits: 794022,
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `creditsUsed`                   | *number*                        | :heavy_check_mark:              | The number of credits remaining |
| `monthlyCredits`                | *number*                        | :heavy_check_mark:              | The number of monthly credits   |