package activities

import "time"

// ShouldProcessEvent decides whether a WorkOS event should be applied to a
// row, guarding against duplicate-apply when the sync replays history.
func ShouldProcessEvent(rowLastEventID *string, rowWorkOSUpdatedAt *time.Time, eventID string, eventUpdatedAt time.Time) bool {
	if rowLastEventID == nil || *rowLastEventID == "" {
		if rowWorkOSUpdatedAt == nil {
			return true
		}
		return !eventUpdatedAt.Before(*rowWorkOSUpdatedAt)
	}
	return eventID > *rowLastEventID
}
