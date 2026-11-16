package assistanttokens

import (
	"errors"
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestKitIdentityValidationError(t *testing.T) {
	t.Parallel()
	t.Run("invalid identity remains unauthorized", func(t *testing.T) {
		t.Parallel()
		cause := fmt.Errorf("identity replaced: %w", assistantidentity.ErrInvalidIdentity)
		err := kitIdentityValidationError(cause)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeUnauthorized, denied.Code)
		require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
	})
	t.Run("database failure retains server error and cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("database unavailable")
		err := kitIdentityValidationError(cause)
		require.ErrorIs(t, err, cause)
		var denied *oops.ShareableError
		require.NotErrorAs(t, err, &denied, "infrastructure failure must not become an authentication refusal")
		require.ErrorContains(t, err, "validate managed assistant identity")
	})
}
