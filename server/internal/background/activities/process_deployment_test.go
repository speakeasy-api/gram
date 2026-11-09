package activities

import (
	"errors"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"testing"
)

func TestProcessDeploymentOtherErrorsUnchanged(t *testing.T) {
	t.Parallel()
	require.NoError(t, deploymentProcessingError(nil))
	transient := errors.New("transient")
	require.Same(t, transient, deploymentProcessingError(transient))
	var app *temporal.ApplicationError
	require.ErrorAs(t, deploymentProcessingError(oops.ErrPermanent), &app)
	require.True(t, app.NonRetryable())
	require.Equal(t, "openapi_doc_error", app.Type())
}
