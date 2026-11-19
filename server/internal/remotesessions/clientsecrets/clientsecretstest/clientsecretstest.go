// Package clientsecretstest builds clientsecrets.Secrets for tests in other
// packages.
package clientsecretstest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// New builds Secrets over conn and the platform key, in a process without
// organization encryption: organizations in platform_key work as in
// production, and any other transition state fails closed.
func New(t *testing.T, conn *pgxpool.Pool, platform *encryption.Client) *clientsecrets.Secrets {
	t.Helper()

	return clientsecrets.WithoutOrganizationEncryption(testenv.NewLogger(t), conn, platform)
}
