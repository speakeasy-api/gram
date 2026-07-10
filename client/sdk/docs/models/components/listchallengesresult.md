# ListChallengesResult

## Example Usage

```typescript
import { ListChallengesResult } from "@gram/client/models/components/listchallengesresult.js";

let value: ListChallengesResult = {
  challenges: [],
  total: 629373,
};
```

## Fields

| Field        | Type                                                                     | Required           | Description                                         |
| ------------ | ------------------------------------------------------------------------ | ------------------ | --------------------------------------------------- |
| `challenges` | [components.AuthzChallenge](../../models/components/authzchallenge.md)[] | :heavy_check_mark: | The challenge events.                               |
| `total`      | _number_                                                                 | :heavy_check_mark: | Total number of matching challenges for pagination. |
