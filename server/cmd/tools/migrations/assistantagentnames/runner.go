// Package assistantagentnames repairs legacy generated names on live assistant agents.
package assistantagentnames

import (
	"context"
	_ "embed"
	"errors"

	"github.com/jackc/pgx/v5"
)

//go:embed queries.sql
var repairSQL string

type DB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Options struct {
	OrganizationID string
	ProjectID      string
	Apply          bool
}

type Report struct{ Agents int64 }

// Run uses one statement so the binding, assistant, and agent stay locked while
// names are copied. Dry runs take the same locks but never execute an UPDATE.
// Errors are deliberately redacted: database errors can contain customer data.
func Run(ctx context.Context, db DB, opts Options) (Report, error) {
	var report Report
	if err := db.QueryRow(ctx, repairSQL, opts.OrganizationID, opts.ProjectID, opts.Apply).Scan(&report.Agents); err != nil {
		return Report{}, errors.New("repair assistant agent names: database statement failed")
	}
	return report, nil
}
