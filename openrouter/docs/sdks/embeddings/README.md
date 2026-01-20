# Embeddings

## Overview

Text embedding endpoints

### Available Operations

* [Generate](#generate) - Submit an embedding request
* [ListModels](#listmodels) - List all embeddings models

## Generate

Submits an embedding request to the embeddings router

### Example Usage

<!-- UsageSnippet language="go" operationID="createEmbeddings" method="post" path="/embeddings" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/openrouter/optionalnullable"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Embeddings.Generate(ctx, operations.CreateEmbeddingsRequest{
        Input: operations.CreateInputUnionStr(
            "<value>",
        ),
        Model: "Taurus",
        Provider: &components.ProviderPreferences{
            DataCollection: optionalnullable.From(openrouter.Pointer(components.DataCollectionAllow.ToPointer())),
            Zdr: optionalnullable.From(openrouter.Pointer(true)),
            EnforceDistillableText: optionalnullable.From(openrouter.Pointer(true)),
            Order: optionalnullable.From(openrouter.Pointer([]components.ProviderPreferencesOrder{
                components.CreateProviderPreferencesOrderProviderName(
                    components.ProviderNameOpenAi,
                ),
            })),
            Only: optionalnullable.From(openrouter.Pointer([]components.ProviderPreferencesOnly{
                components.CreateProviderPreferencesOnlyProviderName(
                    components.ProviderNameOpenAi,
                ),
            })),
            Ignore: optionalnullable.From(openrouter.Pointer([]components.ProviderPreferencesIgnore{
                components.CreateProviderPreferencesIgnoreProviderName(
                    components.ProviderNameOpenAi,
                ),
            })),
            Quantizations: optionalnullable.From(openrouter.Pointer([]components.Quantization{
                components.QuantizationFp16,
            })),
            Sort: optionalnullable.From(openrouter.Pointer(components.CreateProviderPreferencesSortUnionProviderPreferencesProviderSort(
                components.ProviderPreferencesProviderSortPrice,
            ))),
            MaxPrice: &components.ProviderPreferencesMaxPrice{
                Prompt: openrouter.Pointer("1000"),
                Completion: openrouter.Pointer("1000"),
                Image: openrouter.Pointer("1000"),
                Audio: openrouter.Pointer("1000"),
                Request: openrouter.Pointer("1000"),
            },
            PreferredMinThroughput: optionalnullable.From(openrouter.Pointer(components.CreatePreferredMinThroughputNumber(
                100,
            ))),
            PreferredMaxLatency: optionalnullable.From(openrouter.Pointer(components.CreatePreferredMaxLatencyNumber(
                5,
            ))),
        },
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

| Parameter                                                                                | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `ctx`                                                                                    | [context.Context](https://pkg.go.dev/context#Context)                                    | :heavy_check_mark:                                                                       | The context to use for the request.                                                      |
| `request`                                                                                | [operations.CreateEmbeddingsRequest](../../models/operations/createembeddingsrequest.md) | :heavy_check_mark:                                                                       | The request object to use for the request.                                               |
| `opts`                                                                                   | [][operations.Option](../../models/operations/option.md)                                 | :heavy_minus_sign:                                                                       | The options for this request.                                                            |

### Response

**[*operations.CreateEmbeddingsResponse](../../models/operations/createembeddingsresponse.md), error**

### Errors

| Error Type                                | Status Code                               | Content Type                              |
| ----------------------------------------- | ----------------------------------------- | ----------------------------------------- |
| apierrors.BadRequestResponseError         | 400                                       | application/json                          |
| apierrors.UnauthorizedResponseError       | 401                                       | application/json                          |
| apierrors.PaymentRequiredResponseError    | 402                                       | application/json                          |
| apierrors.NotFoundResponseError           | 404                                       | application/json                          |
| apierrors.TooManyRequestsResponseError    | 429                                       | application/json                          |
| apierrors.InternalServerResponseError     | 500                                       | application/json                          |
| apierrors.BadGatewayResponseError         | 502                                       | application/json                          |
| apierrors.ServiceUnavailableResponseError | 503                                       | application/json                          |
| apierrors.EdgeNetworkTimeoutResponseError | 524                                       | application/json                          |
| apierrors.ProviderOverloadedResponseError | 529                                       | application/json                          |
| apierrors.APIError                        | 4XX, 5XX                                  | \*/\*                                     |

## ListModels

Returns a list of all available embeddings models and their properties

### Example Usage

<!-- UsageSnippet language="go" operationID="listEmbeddingsModels" method="get" path="/embeddings/models" -->
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

    res, err := s.Embeddings.ListModels(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.ModelsListResponse != nil {
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

**[*operations.ListEmbeddingsModelsResponse](../../models/operations/listembeddingsmodelsresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.BadRequestResponseError     | 400                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |