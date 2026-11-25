# DeliveryPhase

Whether a failed send was a live hook or a spool-drain replay.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.DeliveryPhaseLive
```


## Values

| Name                 | Value                |
| -------------------- | -------------------- |
| `DeliveryPhaseLive`  | live                 |
| `DeliveryPhaseDrain` | drain                |