# sdk

Developer-friendly & type-safe Go SDK specifically catered to leverage _sdk_ API.

[![Built by Speakeasy](https://img.shields.io/badge/Built_by-SPEAKEASY-374151?style=for-the-badge&labelColor=f3f4f6)](https://www.speakeasy.com/?utm_source=sdk&utm_campaign=go)
[![License: MIT](https://img.shields.io/badge/LICENSE_//_MIT-3b5bdb?style=for-the-badge&labelColor=eff6ff)](https://opensource.org/licenses/MIT)

<br /><br />

> [!IMPORTANT]
> This SDK is not yet ready for production use. To complete setup please follow the steps outlined in your [workspace](https://app.speakeasy.com/org/speakeasy-self/speakeasy-self). Delete this section before > publishing to a package manager.

<!-- Start Summary [summary] -->
## Summary

Gram API Description: Gram is the tools platform for AI agents
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [sdk](#sdk)
  * [SDK Installation](#sdk-installation)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

To add the SDK as a dependency to your project:
```bash
go get github.com/speakeasy-api/gram/hooks/sdk
```
<!-- End SDK Installation [installation] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.IngestHookResult != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security schemes globally:

| Name                           | Type   | Scheme  |
| ------------------------------ | ------ | ------- |
| `ApikeyHeaderGramKey`          | apiKey | API key |
| `ProjectSlugHeaderGramProject` | apiKey | API key |

You can set the security parameters through the `WithSecurity` option when initializing the SDK client instance. The selected scheme will be used by default to authenticate with the API for all operations that support it. For example:
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.IngestHookResult != nil {
		// handle response
	}
}

```

### Per-Operation Security Schemes

Some operations in this SDK require the security scheme to be specified at the request level. For example:
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New()

	res, err := s.Hooks.UploadSkillContent(ctx, operations.UploadSkillContentRequest{
		Body: components.UploadSkillContentPayload{
			Content:       "<value>",
			RawSha256:     "<value>",
			SchemaVersion: components.SchemaVersionHookSkillContentV1,
		},
	}, sdk.Pointer(operations.UploadSkillContentSecurity{}))
	if err != nil {
		log.Fatal(err)
	}
	if res != nil {
		// handle response
	}
}

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [Hooks](docs/sdks/hooks/README.md)

* [Ingest](docs/sdks/hooks/README.md#ingest) - ingest hooks
* [UploadSkillContent](docs/sdks/hooks/README.md#uploadskillcontent) - uploadSkillContent hooks

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `retry.Config` object to the call by using the `WithRetries` option:
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"github.com/speakeasy-api/gram/hooks/sdk/retry"
	"log"
	"models/operations"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	}, operations.WithRetries(
		retry.Config{
			Strategy: "backoff",
			Backoff: &retry.BackoffStrategy{
				InitialInterval: 1,
				MaxInterval:     50,
				Exponent:        1.1,
				MaxElapsedTime:  100,
			},
			RetryConnectionErrors: false,
		}))
	if err != nil {
		log.Fatal(err)
	}
	if res.IngestHookResult != nil {
		// handle response
	}
}

```

If you'd like to override the default retry strategy for all operations that support retries, you can use the `WithRetryConfig` option at SDK initialization:
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"github.com/speakeasy-api/gram/hooks/sdk/retry"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithRetryConfig(
			retry.Config{
				Strategy: "backoff",
				Backoff: &retry.BackoffStrategy{
					InitialInterval: 1,
					MaxInterval:     50,
					Exponent:        1.1,
					MaxElapsedTime:  100,
				},
				RetryConnectionErrors: false,
			}),
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.IngestHookResult != nil {
		// handle response
	}
}

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Handling errors in this SDK should largely match your expectations. All operations return a response object or an error, they will never return both.

By Default, an API error will return `apierrors.APIError`. When custom error responses are specified for an operation, the SDK may also return their associated error. You can refer to respective *Errors* tables in SDK docs for more details on possible error types for each operation.

For example, the `Ingest` function may return the following errors:

| Error Type             | Status Code                       | Content Type     |
| ---------------------- | --------------------------------- | ---------------- |
| apierrors.ServiceError | 400, 401, 403, 404, 409, 415, 422 | application/json |
| apierrors.ServiceError | 500, 502                          | application/json |
| apierrors.APIError     | 4XX, 5XX                          | \*/\*            |

### Example

```go
package main

import (
	"context"
	"errors"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/apierrors"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	})
	if err != nil {

		var e *apierrors.ServiceError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.ServiceError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.APIError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}
	}
}

```
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally using the `WithServerURL(serverURL string)` option when initializing the SDK client instance. For example:
```go
package main

import (
	"context"
	"github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"log"
)

func main() {
	ctx := context.Background()

	s := sdk.New(
		sdk.WithServerURL("https://app.getgram.ai"),
		sdk.WithSecurity(components.Security{
			ApikeyHeaderGramKey:          "<YOUR_API_KEY_HERE>",
			ProjectSlugHeaderGramProject: "<YOUR_API_KEY_HERE>",
		}),
	)

	res, err := s.Hooks.Ingest(ctx, operations.IngestHookEventRequest{
		Body: components.IngestRequestBody{
			Event: components.HookIngestEvent{
				Type: components.TypeSkillActivated,
			},
			SchemaVersion: "<value>",
			Source: components.HookIngestSource{
				Adapter: "<value>",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.IngestHookResult != nil {
		// handle response
	}
}

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The Go SDK makes API calls that wrap an internal HTTP client. The requirements for the HTTP client are very simple. It must match this interface:

```go
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
```

The built-in `net/http` client satisfies this interface and a default client based on the built-in is provided by default. To replace this default with a client of your own, you can implement this interface yourself or provide your own client configured as desired. Here's a simple example, which adds a client with a 30 second timeout.

```go
import (
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/hooks/sdk"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	sdkClient  = sdk.New(sdk.WithClient(httpClient))
)
```

This can be a convenient way to configure timeouts, cookies, proxies, custom headers, and other low-level configuration.
<!-- End Custom HTTP Client [http-client] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation.
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release.

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=sdk&utm_campaign=go)
