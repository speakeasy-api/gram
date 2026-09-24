package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// BuildUserSessionClientView converts a repo client row into the API response
// type. client_secret_hash is intentionally omitted — it is never returned
// over the management API.
//
// client_id_metadata_uri is the CIMD/DCR discriminator: non-null means the row
// was resolved from a Client ID Metadata Document rather than registered via
// RFC 7591 DCR. For a CIMD row it equals client_id (enforced by the
// user_session_clients_client_id_metadata_uri_match_check constraint).
//
// credential_kind is derived rather than echoed: the declared method alone does
// not say what the client will be held to, since a method predating the column
// resolves by whether the row stores a secret. The raw declared value rides
// along beside it for anyone debugging against the spec.
//
// activeSessionCount is supplied by the caller rather than read off the row:
// it is a tally over user_sessions, which the client queries do not join.
func BuildUserSessionClientView(row repo.UserSessionClient, activeSessionCount int32) *types.UserSessionClient {
	return &types.UserSessionClient{
		ID:                             row.ID.String(),
		UserSessionIssuerID:            row.UserSessionIssuerID.String(),
		ClientID:                       row.ClientID,
		ClientIDMetadataURI:            conv.FromPGText[string](row.ClientIDMetadataUri),
		ClientIDMetadataFetchedAt:      conv.PtrEmpty(conv.FromPGTimestamptz(row.ClientIDMetadataFetchedAt)),
		ClientIDMetadataCacheExpiresAt: conv.PtrEmpty(conv.FromPGTimestamptz(row.ClientIDMetadataCacheExpiresAt)),
		ClientIDMetadataEtag:           conv.FromPGText[string](row.ClientIDMetadataEtag),
		ClientName:                     row.ClientName,
		CredentialKind:                 string(clientcred.Resolve(row.TokenEndpointAuthMethod, row.ClientSecretHash.Valid)),
		TokenEndpointAuthMethod:        conv.FromPGText[string](row.TokenEndpointAuthMethod),
		RedirectUris:                   row.RedirectUris,
		ClientIDIssuedAt:               row.ClientIDIssuedAt.Time.Format(time.RFC3339),
		ClientSecretExpiresAt:          conv.PtrEmpty(conv.FromPGTimestamptz(row.ClientSecretExpiresAt)),
		CreatedAt:                      row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                      row.UpdatedAt.Time.Format(time.RFC3339),
		ActiveSessionCount:             int(activeSessionCount),
	}
}
