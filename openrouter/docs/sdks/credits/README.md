# Credits

## Overview

Credit management endpoints

### Available Operations

* [GetCredits](#getcredits) - Get remaining credits
* [CreateCoinbaseCharge](#createcoinbasecharge) - Create a Coinbase charge for crypto payment

## GetCredits

Get total credits purchased and used for the authenticated user. [Provisioning key](/docs/guides/overview/auth/provisioning-api-keys) required.

### Example Usage

<!-- UsageSnippet language="go" operationID="getCredits" method="get" path="/credits" -->
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

    res, err := s.Credits.GetCredits(ctx)
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

**[*operations.GetCreditsResponse](../../models/operations/getcreditsresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.UnauthorizedResponseError   | 401                                   | application/json                      |
| apierrors.ForbiddenResponseError      | 403                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |

## CreateCoinbaseCharge

Create a Coinbase charge for crypto payment

### Example Usage

<!-- UsageSnippet language="go" operationID="createCoinbaseCharge" method="post" path="/credits/coinbase" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New()

    res, err := s.Credits.CreateCoinbaseCharge(ctx, components.CreateChargeRequest{
        Amount: 100,
        Sender: "0x1234567890123456789012345678901234567890",
        ChainID: components.ChainIDOne,
    }, operations.CreateCoinbaseChargeSecurity{
        Bearer: "<YOUR_BEARER_TOKEN_HERE>",
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

| Parameter                                                                                          | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                              | [context.Context](https://pkg.go.dev/context#Context)                                              | :heavy_check_mark:                                                                                 | The context to use for the request.                                                                |
| `request`                                                                                          | [components.CreateChargeRequest](../../models/components/createchargerequest.md)                   | :heavy_check_mark:                                                                                 | The request object to use for the request.                                                         |
| `security`                                                                                         | [operations.CreateCoinbaseChargeSecurity](../../models/operations/createcoinbasechargesecurity.md) | :heavy_check_mark:                                                                                 | The security requirements to use for the request.                                                  |
| `opts`                                                                                             | [][operations.Option](../../models/operations/option.md)                                           | :heavy_minus_sign:                                                                                 | The options for this request.                                                                      |

### Response

**[*operations.CreateCoinbaseChargeResponse](../../models/operations/createcoinbasechargeresponse.md), error**

### Errors

| Error Type                             | Status Code                            | Content Type                           |
| -------------------------------------- | -------------------------------------- | -------------------------------------- |
| apierrors.BadRequestResponseError      | 400                                    | application/json                       |
| apierrors.UnauthorizedResponseError    | 401                                    | application/json                       |
| apierrors.TooManyRequestsResponseError | 429                                    | application/json                       |
| apierrors.InternalServerResponseError  | 500                                    | application/json                       |
| apierrors.APIError                     | 4XX, 5XX                               | \*/\*                                  |