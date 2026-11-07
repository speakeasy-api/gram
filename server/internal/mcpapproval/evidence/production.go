package evidence

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/catalog"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/exposure"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/remoteprobe"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
)

// NewProductionAssembler builds the assembler every MCP review uses. Every
// outbound request goes through guardianPolicy, which refuses internal
// destinations. One probe serves both the authority and tool-declarations
// slots: they are two views of the same remote prober.
func NewProductionAssembler(logger *slog.Logger, db *pgxpool.Pool, guardianPolicy *guardian.Policy, traffic exposure.Reader, mcpCatalog catalog.Catalog, githubToken string) *Assembler {
	prober := remoteprobe.New(logger, guardianPolicy)
	return NewAssembler(
		packagemeta.NewClient(guardianPolicy.PooledClient()),
		repometa.NewClient(guardianPolicy.PooledClient(), repometa.WithToken(githubToken)),
		advisories.NewClient(guardianPolicy.PooledClient()),
		domainmeta.NewClient(guardianPolicy.PooledClient()),
		traffic,
		prober,
		prober,
		catalog.New(logger, db, mcpCatalog),
	)
}
