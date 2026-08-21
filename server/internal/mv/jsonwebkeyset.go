package mv

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	jwks "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	repo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
)

// BuildJsonWebKeySetView converts a set row into its API shape.
func BuildJsonWebKeySetView(set repo.JsonWebKeySet) *jwks.JSONWebKeySet {
	return &jwks.JSONWebKeySet{
		ID:             set.ID.String(),
		OrganizationID: set.OrganizationID,
		ExternalKeyID:  set.ExternalKeyID.String(),
		Name:           set.Name,
		CreatedAt:      set.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      set.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildJsonWebKeySetListView converts set rows into API shapes.
func BuildJsonWebKeySetListView(sets []repo.JsonWebKeySet) []*jwks.JSONWebKeySet {
	result := make([]*jwks.JSONWebKeySet, len(sets))
	for i, set := range sets {
		result[i] = BuildJsonWebKeySetView(set)
	}
	return result
}

// BuildJsonWebKeyView converts a published key row into its API shape. The
// stored public_jwk is passed through as raw JSON rather than decoded and
// re-encoded, so the document verifiers would see is exactly the document the
// API shows.
func BuildJsonWebKeyView(key repo.JsonWebKey) *jwks.JSONWebKey {
	return &jwks.JSONWebKey{
		ID:              key.ID.String(),
		OrganizationID:  key.OrganizationID,
		JSONWebKeySetID: key.JsonWebKeySetID.String(),
		ExternalKeyID:   key.ExternalKeyID.String(),
		Kid:             key.Kid,
		KeyState:        key.State,
		PublicJwk:       json.RawMessage(key.PublicJwk),
		ActivatedAt:     formatOptionalTimestamptz(key.ActivatedAt),
		RetiredAt:       formatOptionalTimestamptz(key.RetiredAt),
		RevokedAt:       formatOptionalTimestamptz(key.RevokedAt),
		CreatedAt:       key.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       key.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildJsonWebKeyListView converts published key rows into API shapes.
func BuildJsonWebKeyListView(keys []repo.JsonWebKey) []*jwks.JSONWebKey {
	result := make([]*jwks.JSONWebKey, len(keys))
	for i, key := range keys {
		result[i] = BuildJsonWebKeyView(key)
	}
	return result
}

func formatOptionalTimestamptz(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}

	formatted := ts.Time.Format(time.RFC3339)
	return &formatted
}
