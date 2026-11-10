package audit

import (
	"errors"
	"testing"
)

func TestMarshalBoundedPage(t *testing.T) {
	for _, tt := range []struct {
		name        string
		value       any
		limit       int
		want        string
		tooLarge    bool
		encodeError bool
	}{
		{name: "exact limit", value: "a", limit: 3, want: `"a"`},
		{name: "below limit", value: []int{}, limit: 3, want: `[]`},
		{name: "over limit", value: "a", limit: 2, tooLarge: true},
		{name: "zero limit", value: nil, limit: 0, tooLarge: true},
		{name: "negative limit", value: nil, limit: -1, tooLarge: true},
		{name: "marshal failure", value: make(chan int), limit: 100, encodeError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalBoundedPage(tt.value, tt.limit)
			if errors.Is(err, ErrPageTooLarge) != tt.tooLarge {
				t.Fatalf("unexpected size error: %v", err)
			}
			if tt.tooLarge || tt.encodeError {
				if err == nil || got != nil {
					t.Fatalf("failed encoding must return no bytes: %q, %v", got, err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
