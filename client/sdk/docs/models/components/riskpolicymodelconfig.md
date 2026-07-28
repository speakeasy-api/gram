# RiskPolicyModelConfig

## Example Usage

```typescript
import { RiskPolicyModelConfig } from "@gram/client/models/components/riskpolicymodelconfig.js";

let value: RiskPolicyModelConfig = {};
```

## Fields

| Field         | Type      | Required           | Description                                                                                                                    |
| ------------- | --------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| `failOpen`    | _boolean_ | :heavy_minus_sign: | When the judge errors or times out: true allows the message (fail-open), false blocks it (fail-closed). Defaults to fail-open. |
| `model`       | _string_  | :heavy_minus_sign: | OpenRouter model id the judge should use. Empty selects the default judge model.                                               |
| `temperature` | _number_  | :heavy_minus_sign: | Sampling temperature for the judge. Defaults to a low value for deterministic verdicts.                                        |
