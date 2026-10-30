package activities

import (
	"errors"
	"fmt"
	"github.com/sourcegraph/conc/pool"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"testing"
)

func TestProcessDeploymentRetiredAggregate(t *testing.T) {
	workers := pool.New().WithErrors()
	workers.Go(func() error { return errors.New("unrelated source failed") })
	workers.Go(func() error { return fmt.Errorf("external attachment: %w", externalmcp.ErrCatalogSourceRetired) })
	workers.Go(func() error { return oops.ErrPermanent })
	workers.Go(func() error { return nil })
	aggregate := workers.Wait()
	require.ErrorIs(t, aggregate, externalmcp.ErrCatalogSourceRetired)
	err := deploymentProcessingError(aggregate)
	var app *temporal.ApplicationError
	require.ErrorAs(t, err, &app)
	require.True(t, app.NonRetryable())
	require.Equal(t, "retired_pulse_catalog", app.Type())
	require.ErrorIs(t, err, externalmcp.ErrCatalogSourceRetired)
}
func TestProcessDeploymentOtherErrorsUnchanged(t *testing.T) {
	require.NoError(t, deploymentProcessingError(nil))
	transient := errors.New("transient")
	require.Same(t, transient, deploymentProcessingError(transient))
	var app *temporal.ApplicationError
	require.ErrorAs(t, deploymentProcessingError(oops.ErrPermanent), &app)
	require.True(t, app.NonRetryable())
	require.Equal(t, "openapi_doc_error", app.Type())
}
