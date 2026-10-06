package remotesessions_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestRegistrationAdmission_SmallPool(t *testing.T) {
	t.Parallel()
	_, ti := newTestServiceWithConfig(t, testServiceConfig{maxDBConns: 4})
	remotesessions.RunRegistrationAdmissionRegression(t, ti.conn)
}
