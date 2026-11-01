package chatredact_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
)

func TestTextMasksPersonalIdentifiers(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "\x00"})
	for name, input := range map[string]string{
		"email":           "Their address is r.qtmzkwa@harborline-freight.test.",
		"phone":           "Phone on file is (312) 555-0142.",
		"dashed phone":    "Call 312-555-0142 after six.",
		"intl phone":      "Reach me on +44 20 7946 0958.",
		"card":            "Card is 4111 1111 1111 1111, expiry next year.",
		"dashed card":     "Card 5555-5555-5555-4444 was declined.",
		"ssn":             "SSN entered was 123-45-6789.",
		"medical record":  "The parser pulled MRN-48213377 as the certificate number.",
		"labelled record": "Medical record number: 00482133 is on file.",
	} {
		got, changed, err := r.Text(t.Context(), input)
		require.NoError(t, err, name)
		require.True(t, changed, name)
		require.Contains(t, got, "[REDACTED]", name)
		for _, secret := range []string{"qtmzkwa", "555-0142", "7946", "1111 1111", "5555-5555", "45-6789", "48213377", "00482133"} {
			require.NotContains(t, got, secret, name)
		}
	}
}

func TestTextKeepsLookalikeNumbers(t *testing.T) {
	t.Parallel()
	r := chatredact.NewWithScanner(wordScanner{word: "\x00"})
	for name, input := range map[string]string{
		"order id":        "Order 4111 1111 1111 1112 shipped.",
		"timestamp ms":    "Event at 1793324886494 failed.",
		"date":            "Deploy on 2026-10-30 at 01:48.",
		"invalid ssn":     "Ticket 900-12-3456 and 123-00-4567 are references.",
		"version":         "Upgraded to 1.27.3 and port 5432.",
		"mrn word":        "The MRN lookup failed again.",
		"bare ten digits": "Shipment 3125550142 left the yard.",
	} {
		got, changed, err := r.Text(t.Context(), input)
		require.NoError(t, err, name)
		require.False(t, changed, name)
		require.Equal(t, input, got, name)
	}
}
