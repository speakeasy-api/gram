# ServiceError

unauthorized access

## Example Usage

```typescript
import { ServiceError } from "@gram/client/models/errors";

// No examples available for this model
```

## Fields

| Field                                                                               | Type                                                                                | Required                                                                            | Description                                                                         | Example                                                                             |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `fault`                                                                             | *boolean*                                                                           | :heavy_check_mark:                                                                  | Is the error a server-side fault?                                                   |                                                                                     |
| `id`                                                                                | *string*                                                                            | :heavy_check_mark:                                                                  | ID is a unique identifier for this particular occurrence of the problem.            | 123abc                                                                              |
| `message`                                                                           | *string*                                                                            | :heavy_check_mark:                                                                  | Message is a human-readable explanation specific to this occurrence of the problem. | parameter 'p' must be an integer                                                    |
| `name`                                                                              | *string*                                                                            | :heavy_check_mark:                                                                  | Name is the name of this class of errors.                                           | bad_request                                                                         |
| `temporary`                                                                         | *boolean*                                                                           | :heavy_check_mark:                                                                  | Is the error temporary?                                                             |                                                                                     |
| `timeout`                                                                           | *boolean*                                                                           | :heavy_check_mark:                                                                  | Is the error a timeout?                                                             |                                                                                     |