package deviceintegrations

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

// testProviderID is a fake inventory-source provider registered once for the
// whole test binary (the registry is process-global).
const testProviderID = "testmdm"

// testSinkProviderID is a fake evidence-sink provider for push tests.
const testSinkProviderID = "testsink"

// testRotateProviderID declares two required secrets for rotation tests.
const testRotateProviderID = "testrotate"

func TestMain(m *testing.M) {
	providers.Register(providers.Descriptor{
		ID:           testProviderID,
		DisplayName:  "Test MDM",
		Capabilities: []providers.Capability{providers.CapabilityInventorySource},
		Fields: []providers.CredentialField{
			{Key: "instance_url", Label: "Instance URL", Kind: providers.FieldKindURL, Secret: false, Required: true},
			{Key: "api_key", Label: "API Key", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: "note", Label: "Note", Kind: providers.FieldKindText, Secret: false, Required: false},
			// Behavior knobs for the fake: parallel tests drive per-config
			// behavior through settings since the registry is process-global.
			{Key: "devices", Label: "Devices", Kind: providers.FieldKindText, Secret: false, Required: false},
			{Key: "fail", Label: "Fail Mode", Kind: providers.FieldKindText, Secret: false, Required: false},
			{Key: "raw_json", Label: "Raw JSON", Kind: providers.FieldKindText, Secret: false, Required: false},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: "testmdm_inventory", Capability: providers.CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps providers.Deps) providers.InventorySource { return fakeInventorySource{} },
		NewEvidenceSink:    nil,
	})
	providers.Register(providers.Descriptor{
		ID:           testRotateProviderID,
		DisplayName:  "Test Rotate",
		Capabilities: []providers.Capability{providers.CapabilityInventorySource},
		// Two required secrets so credential-rotation tests can prove the
		// per-key merge: supplying one secret must keep the other.
		Fields: []providers.CredentialField{
			{Key: "client_id", Label: "Client ID", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: "client_secret", Label: "Client Secret", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: "instance_url", Label: "Instance URL", Kind: providers.FieldKindURL, Secret: false, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: "testrotate_inventory", Capability: providers.CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps providers.Deps) providers.InventorySource { return fakeInventorySource{} },
		NewEvidenceSink:    nil,
	})
	providers.Register(providers.Descriptor{
		ID:           testSinkProviderID,
		DisplayName:  "Test Sink",
		Capabilities: []providers.Capability{providers.CapabilityEvidenceSink},
		Fields: []providers.CredentialField{
			{Key: "api_key", Label: "API Key", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: "fail_push", Label: "Fail Push", Kind: providers.FieldKindText, Secret: false, Required: false},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: "testsink_evidence", Capability: providers.CapabilityEvidenceSink, Interval: time.Hour},
		},
		NewInventorySource: nil,
		NewEvidenceSink:    func(deps providers.Deps) providers.EvidenceSink { return fakeEvidenceSink{} },
	})

	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fakeInventorySource struct{}

func (fakeInventorySource) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	return nil
}

// ListDevices is driven by the config's settings: "fail" forces an error
// ("auth" wraps it as a credential rejection), and "devices" is a
// comma-separated list of external ids, each optionally "id=email".
func (fakeInventorySource) ListDevices(ctx context.Context, creds providers.Credentials, settings providers.Settings, cursor string) (providers.DevicePage, error) {
	switch settings["fail"] {
	case "auth":
		return providers.DevicePage{Devices: nil, NextCursor: ""}, providers.NewAuthError(errors.New("credentials rejected"))
	case "boom":
		return providers.DevicePage{Devices: nil, NextCursor: ""}, errors.New("vendor exploded")
	}
	// raw_json overrides each device's vendor record verbatim, letting tests
	// feed the framework payloads the database may reject (e.g. jsonb
	// refusing a Unicode NUL escape).
	rawRecord := []byte(`{"fake":true}`)
	if override := settings["raw_json"]; override != "" {
		rawRecord = []byte(override)
	}
	var devices []providers.Device
	if raw := settings["devices"]; raw != "" {
		for entry := range strings.SplitSeq(raw, ",") {
			externalID, email, _ := strings.Cut(entry, "=")
			devices = append(devices, providers.Device{
				ExternalID:    externalID,
				SerialNumber:  "SER-" + externalID,
				Hostname:      externalID + ".local",
				OSName:        "macOS",
				OSVersion:     "15.0",
				UserEmail:     email,
				LastCheckInAt: time.Now().UTC(),
				Raw:           rawRecord,
			})
		}
	}
	return providers.DevicePage{Devices: devices, NextCursor: ""}, nil
}

type fakeEvidenceSink struct{}

func (fakeEvidenceSink) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	return nil
}

func (fakeEvidenceSink) PushCoverage(ctx context.Context, creds providers.Credentials, settings providers.Settings, snapshot providers.CoverageSnapshot) error {
	if settings["fail_push"] == "true" {
		return errors.New("push rejected")
	}
	return nil
}

func newStoreTestDB(t *testing.T) (context.Context, *pgxpool.Pool, *Store, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "deviceintegrationstestdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Device Integrations Test",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: orgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	store := NewStore(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t))
	return ctx, conn, store, orgID
}

// upsertTx runs one store upsert inside a committed transaction, the way the
// handler does.
func upsertTx(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string, creds providers.Credentials, settings providers.Settings, enabled bool) (UpsertResult, error) {
	t.Helper()
	return upsertProviderTx(t, ctx, conn, store, orgID, testProviderID, creds, settings, enabled)
}

func upsertProviderTx(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string, providerID string, creds providers.Credentials, settings providers.Settings, enabled bool) (UpsertResult, error) {
	t.Helper()

	desc, ok := providers.Lookup(providerID)
	require.True(t, ok)

	var result UpsertResult
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		result, err = store.upsertWithTx(ctx, tx, desc, orgID, creds, settings, enabled, nil)
		return err
	})
	if err != nil {
		return UpsertResult{}, fmt.Errorf("upsert device integration config: %w", err)
	}
	return result, nil
}

func mustUpsert(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string, creds providers.Credentials, settings providers.Settings, enabled bool) UpsertResult {
	t.Helper()
	result, err := upsertTx(t, ctx, conn, store, orgID, creds, settings, enabled)
	require.NoError(t, err)
	return result
}

func validCreds() providers.Credentials {
	return providers.Credentials{"api_key": "secret-token"}
}

func validSettings() providers.Settings {
	return providers.Settings{"instance_url": "https://example.test"}
}
