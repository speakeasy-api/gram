package deplconfig

import (
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// sharedRetryHTTPClient is a singleton HTTP client with retry logic for remote
// file downloads. This client should be used for all remote source reading.
var sharedRetryHTTPClient = newRetryHTTPClient()

const (
	RETRY_MAX_RETRIES = 3
	RETRY_MIN_BACKOFF = 1 * time.Second
	RETRY_MAX_BACKOFF = 5 * time.Second
	RETRY_TIMEOUT     = 30 * time.Second
)

func newRetryHTTPClient() *http.Client {
	retryClient := retryablehttp.NewClient()

	retryClient.RetryMax = RETRY_MAX_RETRIES
	retryClient.RetryWaitMin = RETRY_MIN_BACKOFF
	retryClient.RetryWaitMax = RETRY_MAX_BACKOFF
	retryClient.HTTPClient.Timeout = RETRY_TIMEOUT
	// To turn off debug logging for the retry client, uncomment the following
	// line. By default, it will emit lines like [DEBUG] GET ...
	// retryClient.Logger = nil

	return retryClient.StandardClient()
}
