# Generations

## Overview

Generation history endpoints

### Available Operations

* [GetGeneration](#getgeneration) - Get request & usage metadata for a generation

## GetGeneration

Get request & usage metadata for a generation

### Example Usage

<!-- UsageSnippet language="go" operationID="getGeneration" method="get" path="/generation" -->
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

    res, err := s.Generations.GetGeneration(ctx, "<id>")
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
| `id`                                                     | *string*                                                 | :heavy_check_mark:                                       | N/A                                                      |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetGenerationResponse](../../models/operations/getgenerationresponse.md), error**

### Errors

| Error Type                                | Status Code                               | Content Type                              |
| ----------------------------------------- | ----------------------------------------- | ----------------------------------------- |
| apierrors.UnauthorizedResponseError       | 401                                       | application/json                          |
| apierrors.PaymentRequiredResponseError    | 402                                       | application/json                          |
| apierrors.NotFoundResponseError           | 404                                       | application/json                          |
| apierrors.TooManyRequestsResponseError    | 429                                       | application/json                          |
| apierrors.InternalServerResponseError     | 500                                       | application/json                          |
| apierrors.BadGatewayResponseError         | 502                                       | application/json                          |
| apierrors.EdgeNetworkTimeoutResponseError | 524                                       | application/json                          |
| apierrors.ProviderOverloadedResponseError | 529                                       | application/json                          |
| apierrors.APIError                        | 4XX, 5XX                                  | \*/\*                                     |