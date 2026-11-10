# HookDeliveryFailureDataCause

Failure class the hook sender observed. http means the control plane answered with a retryable status.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.HookDeliveryFailureDataCauseDNS
```


## Values

| Name                                             | Value                                            |
| ------------------------------------------------ | ------------------------------------------------ |
| `HookDeliveryFailureDataCauseDNS`                | dns                                              |
| `HookDeliveryFailureDataCauseTLS`                | tls                                              |
| `HookDeliveryFailureDataCauseTimeout`            | timeout                                          |
| `HookDeliveryFailureDataCauseCanceled`           | canceled                                         |
| `HookDeliveryFailureDataCauseConnection`         | connection                                       |
| `HookDeliveryFailureDataCauseUnreadableResponse` | unreadable-response                              |
| `HookDeliveryFailureDataCauseUnknown`            | unknown                                          |
| `HookDeliveryFailureDataCauseHTTP`               | http                                             |