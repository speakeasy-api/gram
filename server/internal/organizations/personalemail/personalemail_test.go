package personalemail_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/organizations/personalemail"
)

func TestIsPersonal(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"someone@gmail.com":          true,
		"Someone@GMail.com":          true,
		"someone@googlemail.com":     true,
		"someone@outlook.com":        true,
		"someone@proton.me":          true,
		"someone@yahoo.co.uk":        true,
		"someone@mailinator.com":     true,
		"someone@hey.com":            true,
		"someone@sub.gmail.com":      false,
		"someone@acme.or.ru":         false,
		"someone@gmail.com.":         true,
		"someone@example.com":        false,
		"someone@acme.co.uk":         false,
		"someone@eng.acme.com":       false,
		"someone@speakeasy.com":      false,
		"no-domain":                  false,
		"":                           false,
		"trailing-at@":               false,
		"odd@local@gmail.com":        true,
		"someone@notgmail.com":       false,
		"someone@gmail.com.evil.org": false,
	}
	for email, want := range cases {
		t.Run(email, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, want, personalemail.IsPersonal(email))
		})
	}
}
