package conv

import "github.com/jackc/pgx/v5/pgtype"

func Ptr[T any](v T) *T {
	return &v
}

func FromPGText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}
