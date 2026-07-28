# GetCustomDetectionRuleRequest

## Example Usage

```typescript
import { GetCustomDetectionRuleRequest } from "@gram/client/models/operations/getcustomdetectionrule.js";

let value: GetCustomDetectionRuleRequest = {
  id: "8a2c1c27-51cc-4ede-a4b1-a4402af4418f",
};
```

## Fields

| Field         | Type     | Required           | Description                   |
| ------------- | -------- | ------------------ | ----------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The custom detection rule ID. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                |
