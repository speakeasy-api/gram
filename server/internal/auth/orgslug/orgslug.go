package orgslug

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

type Lookup interface {
	GetOrganizationMetadataBySlug(ctx context.Context, slug string) (orgRepo.OrganizationMetadatum, error)
}

func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugifyRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

const maxSlugAttempts = 10

func FindUnique(ctx context.Context, lookup Lookup, base string) (string, error) {
	candidate := base
	for range maxSlugAttempts {
		_, err := lookup.GetOrganizationMetadataBySlug(ctx, candidate)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("get organization metadata by slug %q: %w", candidate, err)
		}

		suffix, err := randomHexSuffix(4)
		if err != nil {
			return "", fmt.Errorf("generate slug suffix: %w", err)
		}
		candidate = base + "-" + suffix
	}
	return "", errors.New("unable to find unique slug after max attempts")
}

func randomHexSuffix(n int) (string, error) {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b)[:n], nil
}
