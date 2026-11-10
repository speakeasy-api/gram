package audit

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseExactFilter(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := ParseExactFilter(ExactFilter{})
		if err != nil || got.ProjectID.Valid || got.From != nil || got.To != nil {
			t.Fatalf("unexpected empty filter: %+v, %v", got, err)
		}
	})
	t.Run("canonical project and offset window", func(t *testing.T) {
		input := ExactFilter{
			ProjectID: "00000000-0000-0000-0000-000000000001",
			Action:    "future.action", ActingSurface: "future_surface",
			From: "2026-01-01T01:00:00.123+01:00", To: "2026-01-01T01:00:01.123+01:00",
		}
		got, err := ParseExactFilter(input)
		if err != nil {
			t.Fatal(err)
		}
		if !got.ProjectID.Valid || got.ProjectID.UUID.String() != input.ProjectID || got.From == nil || got.To == nil || got.To.Sub(*got.From) != time.Second {
			t.Fatalf("unexpected parsed filter: %+v", got)
		}
	})
	for _, field := range []string{"ActorID", "Action", "SubjectType", "SubjectID", "ActingSurface", "From", "To"} {
		t.Run(field, func(t *testing.T) {
			limit := 256
			if field == "From" || field == "To" {
				limit = 64
			}
			for _, value := range []string{" padded", "padded ", "\t", strings.Repeat("x", limit+1), strings.Repeat("é", limit/2+1)} {
				var input ExactFilter
				reflect.ValueOf(&input).Elem().FieldByName(field).SetString(value)
				if _, err := ParseExactFilter(input); err == nil || !strings.Contains(err.Error(), "provide an unpadded ") {
					t.Errorf("expected bounded unpadded error for %q, got %v", value, err)
				}
			}
			if limit == 256 {
				var input ExactFilter
				reflect.ValueOf(&input).Elem().FieldByName(field).SetString(strings.Repeat("x", limit))
				if _, err := ParseExactFilter(input); err != nil {
					t.Errorf("exact byte limit rejected: %v", err)
				}
			}
		})
	}
	for _, project := range []string{"invalid", "00000000000000000000000000000001", "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA", " 00000000-0000-0000-0000-000000000001"} {
		if _, err := ParseExactFilter(ExactFilter{ProjectID: project}); err == nil || err.Error() != "provide a canonical project UUID" {
			t.Errorf("project %q: %v", project, err)
		}
	}
	for _, input := range []ExactFilter{
		{From: "invalid"}, {To: "invalid"},
		{From: "2026-01-01T00:00:00Z", To: "2026-01-01T00:00:00Z"},
		{From: "2026-01-02T00:00:00Z", To: "2026-01-01T00:00:00Z"},
	} {
		if _, err := ParseExactFilter(input); err == nil || err.Error() != "provide valid RFC3339 timestamps with from before to" {
			t.Errorf("window %+v: %v", input, err)
		}
	}
	for _, input := range []ExactFilter{{From: "2026-01-01T00:00:00Z"}, {To: "2026-01-01T00:00:00Z"}} {
		if _, err := ParseExactFilter(input); err != nil {
			t.Errorf("one-sided window %+v: %v", input, err)
		}
	}
}
