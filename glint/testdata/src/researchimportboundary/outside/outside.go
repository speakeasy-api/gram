package outside

import (
	"database/sql"

	"github.com/speakeasy-api/gram/example/repo"
)

// Packages outside the boundary import data stores freely.
func use() {
	_ = sql.ErrNoRows
	_ = repo.Marker
}
