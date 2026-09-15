package agent

import (
	"database/sql" // want `import of "database/sql" is forbidden here`
	"strings"

	"github.com/jackc/pgx/v5"                    // want `import of "github.com/jackc/pgx/v5" is forbidden here`
	"github.com/speakeasy-api/gram/example/repo" // want `import of "github.com/speakeasy-api/gram/example/repo" is forbidden here`
)

func use() {
	_ = strings.TrimSpace("")
	_ = sql.ErrNoRows
	_ = pgx.ErrNoRows
	_ = repo.Marker
}
