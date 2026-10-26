# FailedEventType

Canonical type of the event whose delivery failed. Bounded to the hook activity event vocabulary because it is a metric dimension; a report never describes another report.

## Example Usage

```go
import (
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

value := components.FailedEventTypeSessionStarted
```


## Values

| Name                                  | Value                                 |
| ------------------------------------- | ------------------------------------- |
| `FailedEventTypeSessionStarted`       | session.started                       |
| `FailedEventTypeSessionUpdated`       | session.updated                       |
| `FailedEventTypeSessionEnded`         | session.ended                         |
| `FailedEventTypeMcpInventory`         | mcp.inventory                         |
| `FailedEventTypePromptSubmitted`      | prompt.submitted                      |
| `FailedEventTypeToolRequested`        | tool.requested                        |
| `FailedEventTypeToolCompleted`        | tool.completed                        |
| `FailedEventTypeToolFailed`           | tool.failed                           |
| `FailedEventTypeAssistantResponded`   | assistant.responded                   |
| `FailedEventTypeAssistantThought`     | assistant.thought                     |
| `FailedEventTypeUsageReported`        | usage.reported                        |
| `FailedEventTypeSkillActivated`       | skill.activated                       |
| `FailedEventTypeNotificationReported` | notification.reported                 |