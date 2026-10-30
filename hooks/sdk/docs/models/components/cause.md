# Cause

Failure class the hook sender observed. http means the control plane answered with a retryable status.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.CauseDNS
```


## Values

| Name                      | Value                     |
| ------------------------- | ------------------------- |
| `CauseDNS`                | dns                       |
| `CauseTLS`                | tls                       |
| `CauseTimeout`            | timeout                   |
| `CauseCanceled`           | canceled                  |
| `CauseConnection`         | connection                |
| `CauseUnreadableResponse` | unreadable-response       |
| `CauseUnknown`            | unknown                   |
| `CauseHTTP`               | http                      |