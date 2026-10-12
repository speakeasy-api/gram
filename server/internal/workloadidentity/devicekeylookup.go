package workloadidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// KeySourceDeviceKeys marks the issuer Speakeasy creates for devices with no
// platform identity. Its assertions verify against the key registered for each
// device subject instead of a remote key set.
const KeySourceDeviceKeys = "device_keys"

// ErrDeviceKeyNotFound reports that no live key is registered for the subject.
var ErrDeviceKeyNotFound = errors.New("no device key is registered for this subject")

// DeviceKeyParams addresses one device's registered key. Every field is part of
// the key; omitting one never widens the search.
type DeviceKeyParams struct {
	OrganizationID   string
	WorkloadIssuerID uuid.UUID
	Subject          string
}

// IsDeviceKeyIssuer reports whether an issuer's assertions verify against
// per-device keys rather than its jwks_uri.
func IsDeviceKeyIssuer(issuer repo.WorkloadIssuer) bool {
	return issuer.KeySource.Valid && issuer.KeySource.String == KeySourceDeviceKeys
}

// DeviceKey is a device's registered public key: a one-key JWK Set document to
// verify its assertions against, and the thumbprint they must name as kid.
type DeviceKey struct {
	KeySet     json.RawMessage
	Thumbprint string
}

// DeviceKeySet returns the device's registered public key.
func DeviceKeySet(ctx context.Context, db repo.DBTX, params DeviceKeyParams) (DeviceKey, error) {
	switch {
	case params.OrganizationID == "", params.WorkloadIssuerID == uuid.Nil, params.Subject == "":
		return DeviceKey{}, ErrDeviceKeyNotFound
	}

	row, err := repo.New(db).GetWorkloadDeviceKey(ctx, repo.GetWorkloadDeviceKeyParams{
		OrganizationID:   params.OrganizationID,
		WorkloadIssuerID: params.WorkloadIssuerID,
		Subject:          params.Subject,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return DeviceKey{}, ErrDeviceKeyNotFound
	case err != nil:
		return DeviceKey{}, fmt.Errorf("get workload device key: %w", err)
	}

	set, err := json.Marshal(struct {
		Keys []json.RawMessage `json:"keys"`
	}{Keys: []json.RawMessage{row.PublicJwk}})
	if err != nil {
		return DeviceKey{}, fmt.Errorf("encode workload device key set: %w", err)
	}
	return DeviceKey{KeySet: set, Thumbprint: row.Thumbprint}, nil
}
