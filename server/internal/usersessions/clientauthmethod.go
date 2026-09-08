package usersessions

import (
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// CIMDDocumentDowngradesAuthMethod reports whether persisting doc over row
// would move a client off private_key_jwt. Only that direction is refused: a
// public client upgrading to asymmetric authentication is exactly the change
// the method column exists to record, while the reverse is what a document
// host under someone else's control would publish.
func CIMDDocumentDowngradesAuthMethod(row *repo.UserSessionClient, doc *cimd.Document) bool {
	return row.TokenEndpointAuthMethod.Valid &&
		row.TokenEndpointAuthMethod.String == oauthwire.AuthMethodPrivateKeyJWT &&
		doc.DeclaredAuthMethod() != oauthwire.AuthMethodPrivateKeyJWT
}
