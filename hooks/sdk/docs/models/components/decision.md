# Decision

Whether the local hook should allow or deny the action.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.DecisionAllow

// Open enum: custom values can be created with a direct type cast
custom := components.Decision("custom_value")
```


## Values

| Name            | Value           |
| --------------- | --------------- |
| `DecisionAllow` | allow           |
| `DecisionDeny`  | deny            |