package platformmcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// newDeliveryAuthz builds the client role delivery evaluates loaded role
// grants with. Loaded-grant evaluation never reads the database, and the
// parity client fails the test if the engine and the registry disagree.
func newDeliveryAuthz(t *testing.T) *authz.Client {
	t.Helper()
	return authztest.NewParityClient(t, authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()))
}

// newGrantsAuthz builds a parity client for services that only decide over
// grants already in hand, which never reach the database.
func newGrantsAuthz(t *testing.T) *authz.Client {
	t.Helper()
	return newDeliveryAuthz(t)
}
