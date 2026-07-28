# Type

Canonical Gram hook event type.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.TypeSessionStarted
```


## Values

| Name                       | Value                      |
| -------------------------- | -------------------------- |
| `TypeSessionStarted`       | session.started            |
| `TypeSessionUpdated`       | session.updated            |
| `TypeSessionEnded`         | session.ended              |
| `TypePromptSubmitted`      | prompt.submitted           |
| `TypeToolRequested`        | tool.requested             |
| `TypeToolCompleted`        | tool.completed             |
| `TypeToolFailed`           | tool.failed                |
| `TypeAssistantResponded`   | assistant.responded        |
| `TypeAssistantThought`     | assistant.thought          |
| `TypeUsageReported`        | usage.reported             |
| `TypeSkillActivated`       | skill.activated            |
| `TypeNotificationReported` | notification.reported      |