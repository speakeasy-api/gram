package deployments

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/functions"
)

const (
	maxDisplayNameLength = 128
	maxSlugLength        = 128
)

var (
	ErrRequired           = errors.New("field is required")
	ErrSlug               = errors.New(constants.SlugMessage)
	ErrTooLong            = errors.New("field is too long")
	ErrUnsupported        = errors.New("field value is unsupported")
	ErrExactlyOneRequired = errors.New("one of these fields must be provided, but not both")
)

type maskingError struct {
	wrapped error
	msg     string
}

func (e *maskingError) Error() string { return e.msg }
func (e *maskingError) Unwrap() error { return e.wrapped }

func newErrTooLong(maxLen int) error {
	return &maskingError{
		wrapped: ErrTooLong,
		msg:     fmt.Sprintf("max length is %d", maxLen),
	}
}

func newErrUnsupported(supported string) error {
	return &maskingError{
		wrapped: ErrUnsupported,
		msg:     fmt.Sprintf("unsupported value (allowed values are: %s)", supported),
	}
}

func requireOrElse(acc error, node string, prop string, condition bool, err error) error {
	if !condition {
		return errors.Join(acc, fmt.Errorf("%s/%s: %w", node, prop, err))
	}
	return acc
}

func requireFieldsOrElse(acc error, node string, props []string, condition bool, err error) error {
	if !condition {
		return errors.Join(acc, fmt.Errorf("%s/{%s}: %w", node, strings.Join(props, ","), err))
	}
	return acc
}

func validateUpserts(
	openAPIv3ToUpsert []upsertOpenAPIv3,
	functionsToUpsert []upsertFunctions,
	externalMCPsToUpsert []upsertExternalMCP,
) (err error) {
	for i, a := range openAPIv3ToUpsert {
		node := fmt.Sprintf("openapi/%d", i)
		err = requireOrElse(err, node, "asset_id", a.assetID != uuid.Nil, ErrRequired)

		err = requireOrElse(err, node, "name", len(a.name) <= maxDisplayNameLength, newErrTooLong(maxDisplayNameLength))

		err = requireOrElse(err, node, "slug", constants.SlugPatternRE.MatchString(a.slug), ErrSlug)
		err = requireOrElse(err, node, "slug", len(a.slug) <= maxSlugLength, newErrTooLong(maxSlugLength))
	}

	supportedRuntimes := functions.SupportedRuntimes()
	for i, a := range functionsToUpsert {
		node := fmt.Sprintf("functions/%d", i)
		err = requireOrElse(err, node, "asset_id", a.assetID != uuid.Nil, ErrRequired)

		err = requireOrElse(err, node, "name", len(a.name) <= maxDisplayNameLength, newErrTooLong(maxDisplayNameLength))

		err = requireOrElse(err, node, "slug", constants.SlugPatternRE.MatchString(a.slug), ErrSlug)
		err = requireOrElse(err, node, "slug", len(a.slug) <= maxSlugLength, newErrTooLong(maxSlugLength))

		err = requireOrElse(err, node, "runtime", a.runtime != "", ErrRequired)
		err = requireOrElse(err, node, "runtime", functions.IsSupportedRuntime(a.runtime), newErrUnsupported(supportedRuntimes.String()))
	}

	for i, e := range externalMCPsToUpsert {
		node := fmt.Sprintf("external_mcp/%d", i)
		err = requireFieldsOrElse(err, node,
			[]string{"registry_id", "organization_mcp_collection_registry_id"},
			e.registryID.Valid != e.organizationMcpCollectionRegistryID.Valid,
			ErrExactlyOneRequired)
	}

	return err
}
