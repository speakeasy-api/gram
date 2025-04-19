# Instances
(*instances*)

## Overview

Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.

### Available Operations

* [get_by_slug](#get_by_slug) - getInstance instances

## get_by_slug

Load all relevant data for an instance of a toolset and environment

### Example Usage

```python
import gram_ai
from gram_ai import GramAPI


with GramAPI() as gram_api:

    res = gram_api.instances.get_by_slug(security=gram_ai.GetInstanceSecurity(
        option1=gram_ai.GetInstanceSecurityOption1(
            project_slug_header_gram_project="<YOUR_API_KEY_HERE>",
            session_header_gram_session="<YOUR_API_KEY_HERE>",
        ),
    ), toolset_slug="<value>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `security`                                                          | [models.GetInstanceSecurity](../../models/getinstancesecurity.md)   | :heavy_check_mark:                                                  | N/A                                                                 |
| `toolset_slug`                                                      | *str*                                                               | :heavy_check_mark:                                                  | The slug of the toolset to load                                     |
| `environment_slug`                                                  | *Optional[str]*                                                     | :heavy_minus_sign:                                                  | The slug of the environment to load                                 |
| `gram_session`                                                      | *Optional[str]*                                                     | :heavy_minus_sign:                                                  | Session header                                                      |
| `gram_project`                                                      | *Optional[str]*                                                     | :heavy_minus_sign:                                                  | project header                                                      |
| `gram_key`                                                          | *Optional[str]*                                                     | :heavy_minus_sign:                                                  | API Key header                                                      |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.GetInstanceResult](../../models/getinstanceresult.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| models.APIError | 4XX, 5XX        | \*/\*           |