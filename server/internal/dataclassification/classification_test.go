package dataclassification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dataclassification"
)

func TestParse(t *testing.T) {
	t.Parallel()

	valid := map[string]dataclassification.Class{
		"@access: confidential":                                dataclassification.Confidential,
		"Primary key.\n@access: confidential":                  dataclassification.Confidential,
		"Owner email.\r\n\t@access: confidential-pii \t\r\n\n": dataclassification.ConfidentialPII,
		"@access: secret-restricted":                           dataclassification.SecretRestricted,
		"@access: restricted":                                  dataclassification.Restricted,
		"Raw message text.\n@access: opaque-restricted":        dataclassification.OpaqueRestricted,
	}
	for comment, want := range valid {
		got, err := dataclassification.Parse(comment)
		require.NoError(t, err, comment)
		require.Equal(t, want, got, comment)
	}

	invalid := []string{
		"",
		"A description without a class.",
		"@access: public",
		"@access: Confidential",
		"@access: safe",
		"@access: pii",
		"@access: secret",
		"@access: confidential,confidential-pii",
		"@access: confidential extra",
		"@access:",
		"@access: confidential\nTrailing prose.",
		"@access: confidential\n@access: confidential",
		"@access: confidential\n@access: confidential-pii",
		"Prefix @access: confidential",
	}
	for _, comment := range invalid {
		_, err := dataclassification.Parse(comment)
		require.Error(t, err, comment)
	}
}
