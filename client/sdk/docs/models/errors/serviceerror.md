# ServiceError

unauthorized access

## Example Usage

```typescript
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";

// No examples available for this model
```

## Fields

| Field       | Type      | Required           | Description                                                                         | Example                          |
| ----------- | --------- | ------------------ | ----------------------------------------------------------------------------------- | -------------------------------- |
| `fault`     | _boolean_ | :heavy_check_mark: | Is the error a server-side fault?                                                   |                                  |
| `id`        | _string_  | :heavy_check_mark: | ID is a unique identifier for this particular occurrence of the problem.            | 123abc                           |
| `message`   | _string_  | :heavy_check_mark: | Message is a human-readable explanation specific to this occurrence of the problem. | parameter 'p' must be an integer |
| `name`      | _string_  | :heavy_check_mark: | Name is the name of this class of errors.                                           | bad_request                      |
| `temporary` | _boolean_ | :heavy_check_mark: | Is the error temporary?                                                             |                                  |
| `timeout`   | _boolean_ | :heavy_check_mark: | Is the error a timeout?                                                             |                                  |
