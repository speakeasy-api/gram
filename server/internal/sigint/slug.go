package sigint

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func normalizeSlug(value *types.Slug, name string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(string(conv.PtrValOr(value, ""))))
	if slug == "" {
		slug = conv.ToSlug(name)
		if len(slug) > 40 {
			slug = strings.TrimRight(slug[:40], "-")
		}
	}
	if len(slug) > 40 || !constants.SlugPatternRE.MatchString(slug) {
		return "", fmt.Errorf("slug must contain 1 to 40 lowercase letters, digits, hyphens or underscores")
	}
	return slug, nil
}

func isSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation &&
		(pgErr.ConstraintName == "sigint_custom_signals_project_id_slug_key" || pgErr.ConstraintName == "sigint_sensors_project_id_slug_key")
}
