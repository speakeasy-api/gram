package idjag

import "github.com/google/uuid"

type issuerCacheIdentity struct {
	organizationID string
	id             uuid.UUID
	issuer         string
	uri            string
}
