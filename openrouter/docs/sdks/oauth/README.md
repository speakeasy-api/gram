# OAuth

## Overview

OAuth authentication endpoints

### Available Operations

* [ExchangeAuthCodeForAPIKey](#exchangeauthcodeforapikey) - Exchange authorization code for API key
* [CreateAuthCode](#createauthcode) - Create authorization code

## ExchangeAuthCodeForAPIKey

Exchange an authorization code from the PKCE flow for a user-controlled API key

### Example Usage

<!-- UsageSnippet language="go" operationID="exchangeAuthCodeForAPIKey" method="post" path="/auth/keys" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/operations"
	"github.com/speakeasy-api/gram/openrouter/optionalnullable"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.OAuth.ExchangeAuthCodeForAPIKey(ctx, operations.ExchangeAuthCodeForAPIKeyRequest{
        Code: "auth_code_abc123def456",
        CodeVerifier: openrouter.Pointer("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"),
        CodeChallengeMethod: optionalnullable.From(openrouter.Pointer(operations.ExchangeAuthCodeForAPIKeyCodeChallengeMethodS256.ToPointer())),
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

| Parameter                                                                                                  | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                      | [context.Context](https://pkg.go.dev/context#Context)                                                      | :heavy_check_mark:                                                                                         | The context to use for the request.                                                                        |
| `request`                                                                                                  | [operations.ExchangeAuthCodeForAPIKeyRequest](../../models/operations/exchangeauthcodeforapikeyrequest.md) | :heavy_check_mark:                                                                                         | The request object to use for the request.                                                                 |
| `opts`                                                                                                     | [][operations.Option](../../models/operations/option.md)                                                   | :heavy_minus_sign:                                                                                         | The options for this request.                                                                              |

### Response

**[*operations.ExchangeAuthCodeForAPIKeyResponse](../../models/operations/exchangeauthcodeforapikeyresponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.BadRequestResponseError     | 400                                   | application/json                      |
| apierrors.ForbiddenResponseError      | 403                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |

## CreateAuthCode

Create an authorization code for the PKCE flow to generate a user-controlled API key

### Example Usage

<!-- UsageSnippet language="go" operationID="createAuthKeysCode" method="post" path="/auth/keys/code" -->
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

    res, err := s.OAuth.CreateAuthCode(ctx, operations.CreateAuthKeysCodeRequest{
        CallbackURL: "https://myapp.com/auth/callback",
        CodeChallenge: openrouter.Pointer("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"),
        CodeChallengeMethod: operations.CreateAuthKeysCodeCodeChallengeMethodS256.ToPointer(),
        Limit: openrouter.Pointer[float64](100),
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

| Parameter                                                                                    | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `ctx`                                                                                        | [context.Context](https://pkg.go.dev/context#Context)                                        | :heavy_check_mark:                                                                           | The context to use for the request.                                                          |
| `request`                                                                                    | [operations.CreateAuthKeysCodeRequest](../../models/operations/createauthkeyscoderequest.md) | :heavy_check_mark:                                                                           | The request object to use for the request.                                                   |
| `opts`                                                                                       | [][operations.Option](../../models/operations/option.md)                                     | :heavy_minus_sign:                                                                           | The options for this request.                                                                |

### Response

**[*operations.CreateAuthKeysCodeResponse](../../models/operations/createauthkeyscoderesponse.md), error**

### Errors

| Error Type                            | Status Code                           | Content Type                          |
| ------------------------------------- | ------------------------------------- | ------------------------------------- |
| apierrors.BadRequestResponseError     | 400                                   | application/json                      |
| apierrors.UnauthorizedResponseError   | 401                                   | application/json                      |
| apierrors.InternalServerResponseError | 500                                   | application/json                      |
| apierrors.APIError                    | 4XX, 5XX                              | \*/\*                                 |