# Models

## Overview

Model information endpoints

### Available Operations

* [Count](#count) - Get total count of available models
* [List](#list) - List all models and their properties
* [ListForUser](#listforuser) - List models filtered by user provider preferences

## Count

Get total count of available models

### Example Usage

<!-- UsageSnippet language="go" operationID="listModelsCount" method="get" path="/models/count" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Models.Count(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.ModelsCountResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.ListModelsCountResponse](../../models/operations/listmodelscountresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |

## List

List all models and their properties

### Example Usage

<!-- UsageSnippet language="go" operationID="getModels" method="get" path="/models" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Models.List(ctx, operations.CategoryProgramming.ToPointer(), nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.ModelsListResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                   | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `ctx`                                                       | [context.Context](https://pkg.go.dev/context#Context)       | :heavy_check_mark:                                          | The context to use for the request.                         |                                                             |
| `category`                                                  | [*operations.Category](../../models/operations/category.md) | :heavy_minus_sign:                                          | Filter models by use case category                          | programming                                                 |
| `supportedParameters`                                       | **string*                                                   | :heavy_minus_sign:                                          | N/A                                                         |                                                             |
| `opts`                                                      | [][operations.Option](../../models/operations/option.md)    | :heavy_minus_sign:                                          | The options for this request.                               |                                                             |

### Response

**[*operations.GetModelsResponse](../../models/operations/getmodelsresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.BadRequestResponseError     | 400                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |

## ListForUser

List models filtered by user provider preferences

### Example Usage

<!-- UsageSnippet language="go" operationID="listModelsUser" method="get" path="/models/user" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New()

    res, err := s.Models.ListForUser(ctx, operations.ListModelsUserSecurity{
        Bearer: "<YOUR_BEARER_TOKEN_HERE>",
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.ModelsListResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                              | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `ctx`                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                  | :heavy_check_mark:                                                                     | The context to use for the request.                                                    |
| `security`                                                                             | [operations.ListModelsUserSecurity](../../models/operations/listmodelsusersecurity.md) | :heavy_check_mark:                                                                     | The security requirements to use for the request.                                      |
| `opts`                                                                                 | [][operations.Option](../../models/operations/option.md)                               | :heavy_minus_sign:                                                                     | The options for this request.                                                          |

### Response

**[*operations.ListModelsUserResponse](../../models/operations/listmodelsuserresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.UnauthorizedResponseError   | 401                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |