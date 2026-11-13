# Decision

Whether the local hook should allow or deny the action. "ask" (tool calls from Claude Cowork only) asks the person to allow the call in the agent's own permission prompt; a client that does not support it must deny.

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
| `DecisionAsk`   | ask             |