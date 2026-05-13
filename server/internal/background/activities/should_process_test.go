package activities_test

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestShouldProcessEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)
	eventID1 := "event_01HZ001"
	eventID2 := "event_01HZ002"
	emptyEventID := ""
	cases := []struct {
		name             string
		rowLastEventID   *string
		rowWorkOSUpdated *time.Time
		eventID          string
		eventUpdated     time.Time
		want             bool
	}{
		{
			name: "no row state — apply",
			want: true,
		},
		{
			name:             "no last_event_id, event updated_at == row updated_at — apply",
			rowWorkOSUpdated: &now,
			eventUpdated:     now,
			want:             true,
		},
		{
			name:             "no last_event_id, event newer than row — apply",
			rowWorkOSUpdated: &now,
			eventUpdated:     later,
			want:             true,
		},
		{
			name:             "no last_event_id, event older than row — skip",
			rowWorkOSUpdated: &now,
			eventUpdated:     earlier,
			want:             false,
		},
		{
			name:           "last_event_id set, event ID strictly greater — apply",
			rowLastEventID: &eventID1,
			eventID:        "event_01HZ002",
			want:           true,
		},
		{
			name:           "last_event_id set, event ID equal — skip",
			rowLastEventID: &eventID1,
			eventID:        "event_01HZ001",
			want:           false,
		},
		{
			name:           "last_event_id set, event ID smaller — skip",
			rowLastEventID: &eventID2,
			eventID:        "event_01HZ001",
			want:           false,
		},
		{
			name:           "empty string last_event_id treated as missing",
			rowLastEventID: &emptyEventID,
			eventID:        "event_01HZ001",
			want:           true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := activities.ShouldProcessEvent(tc.rowLastEventID, tc.rowWorkOSUpdated, tc.eventID, tc.eventUpdated)
			if got != tc.want {
				t.Fatalf("ShouldProcessEvent() = %v, want %v", got, tc.want)
			}
		})
	}
}
