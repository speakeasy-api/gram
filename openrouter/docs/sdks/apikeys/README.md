# APIKeys

## Overview

API key management endpoints

### Available Operations

* [List](#list) - List API keys
* [Create](#create) - Create a new API key
* [Update](#update) - Update an API key
* [Delete](#delete) - Delete an API key
* [Get](#get) - Get a single API key
* [GetCurrentKeyMetadata](#getcurrentkeymetadata) - Get current API key

## List

List all API keys for the authenticated user. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="list" method="get" path="/keys" -->
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

    res, err := s.APIKeys.List(ctx, openrouter.Pointer("false"), openrouter.Pointer("0"))
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              | Example                                                  |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |                                                          |
| `includeDisabled`                                        | **string*                                                | :heavy_minus_sign:                                       | Whether to include disabled API keys in the response     | false                                                    |
| `offset`                                                 | **string*                                                | :heavy_minus_sign:                                       | Number of API keys to skip for pagination                | 0                                                        |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |                                                          |

### Response

**[*operations.ListResponse](../../models/operations/listresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |

## Create

Create a new API key for the authenticated user. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="createKeys" method="post" path="/keys" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/optionalnullable"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"github.com/speakeasy-api/gram/openrouter/types"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.APIKeys.Create(ctx, operations.CreateKeysRequest{
        Name: "My New API Key",
        Limit: optionalnullable.From(openrouter.Pointer[float64](50)),
        LimitReset: optionalnullable.From(openrouter.Pointer(operations.CreateKeysLimitResetMonthly.ToPointer())),
        IncludeByokInLimit: openrouter.Pointer(true),
        ExpiresAt: optionalnullable.From(openrouter.Pointer(types.MustNewTimeFromString("2027-12-31T23:59:59Z"))),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                    | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `ctx`                                                                        | [context.Context](https://pkg.go.dev/context#Context)                        | :heavy_check_mark:                                                           | The context to use for the request.                                          |
| `request`                                                                    | [operations.CreateKeysRequest](../../models/operations/createkeysrequest.md) | :heavy_check_mark:                                                           | The request object to use for the request.                                   |
| `opts`                                                                       | [][operations.Option](../../models/operations/option.md)                     | :heavy_minus_sign:                                                           | The options for this request.                                                |

### Response

**[*operations.CreateKeysResponse](../../models/operations/createkeysresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.BadRequestResponseError      | 400                                    | application/json                       |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |

## Update

Update an existing API key. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="updateKeys" method="patch" path="/keys/{hash}" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/optionalnullable"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.APIKeys.Update(ctx, "f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943", operations.UpdateKeysRequestBody{
        Name: openrouter.Pointer("Updated API Key Name"),
        Disabled: openrouter.Pointer(false),
        Limit: optionalnullable.From(openrouter.Pointer[float64](75)),
        LimitReset: optionalnullable.From(openrouter.Pointer(operations.UpdateKeysLimitResetDaily.ToPointer())),
        IncludeByokInLimit: openrouter.Pointer(true),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                                                 | Type                                                                                                                      | Required                                                                                                                  | Description                                                                                                               | Example                                                                                                                   |
| ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                                     | [context.Context](https://pkg.go.dev/context#Context)                                                                     | :heavy_check_mark:                                                                                                        | The context to use for the request.                                                                                       |                                                                                                                           |
| `hash`                                                                                                                    | *string*                                                                                                                  | :heavy_check_mark:                                                                                                        | The hash identifier of the API key to update                                                                              | f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943                                                          |
| `body`                                                                                                                    | [operations.UpdateKeysRequestBody](../../models/operations/updatekeysrequestbody.md)                                      | :heavy_check_mark:                                                                                                        | N/A                                                                                                                       | {<br/>"name": "Updated API Key Name",<br/>"disabled": false,<br/>"limit": 75,<br/>"limit_reset": "daily",<br/>"include_byok_in_limit": true<br/>} |
| `opts`                                                                                                                    | [][operations.Option](../../models/operations/option.md)                                                                  | :heavy_minus_sign:                                                                                                        | The options for this request.                                                                                             |                                                                                                                           |

### Response

**[*operations.UpdateKeysResponse](../../models/operations/updatekeysresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.BadRequestResponseError      | 400                                    | application/json                       |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.NotFoundResponseError        | 404                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |

## Delete

Delete an existing API key. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="deleteKeys" method="delete" path="/keys/{hash}" -->
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

    res, err := s.APIKeys.Delete(ctx, "f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943")
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                        | Type                                                             | Required                                                         | Description                                                      | Example                                                          |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `ctx`                                                            | [context.Context](https://pkg.go.dev/context#Context)            | :heavy_check_mark:                                               | The context to use for the request.                              |                                                                  |
| `hash`                                                           | *string*                                                         | :heavy_check_mark:                                               | The hash identifier of the API key to delete                     | f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943 |
| `opts`                                                           | [][operations.Option](../../models/operations/option.md)         | :heavy_minus_sign:                                               | The options for this request.                                    |                                                                  |

### Response

**[*operations.DeleteKeysResponse](../../models/operations/deletekeysresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.NotFoundResponseError        | 404                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |

## Get

Get a single API key by hash. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="getKey" method="get" path="/keys/{hash}" -->
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

    res, err := s.APIKeys.Get(ctx, "f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943")
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                        | Type                                                             | Required                                                         | Description                                                      | Example                                                          |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `ctx`                                                            | [context.Context](https://pkg.go.dev/context#Context)            | :heavy_check_mark:                                               | The context to use for the request.                              |                                                                  |
| `hash`                                                           | *string*                                                         | :heavy_check_mark:                                               | The hash identifier of the API key to retrieve                   | f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943 |
| `opts`                                                           | [][operations.Option](../../models/operations/option.md)         | :heavy_minus_sign:                                               | The options for this request.                                    |                                                                  |

### Response

**[*operations.GetKeyResponse](../../models/operations/getkeyresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.NotFoundResponseError        | 404                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |

## GetCurrentKeyMetadata

Get information on the API key associated with the current authentication session

### Example Usage

<!-- UsageSnippet language="go" operationID="getCurrentKey" method="get" path="/key" -->
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

    res, err := s.APIKeys.GetCurrentKeyMetadata(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
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

**[*operations.GetCurrentKeyResponse](../../models/operations/getcurrentkeyresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.UnauthorizedResponseError   | 401                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |