package deviceintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// minSecretLength is the floor for supplied secret values. It exists for
// the error-scrubbing guarantee: sanitizeSyncError redacts stored values of
// at least four characters, so enforcing a larger floor here means every
// stored secret is always scrubbable.
const minSecretLength = 8

// Store owns persistence for device integration configs, schedules, and sync
// state. Secret credentials are stored as one encrypted JSON document; the
// plaintext exists only transiently inside store methods and their callers,
// never in API responses, logs, or Temporal payloads.
type Store struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
}

func NewStore(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Store {
	return &Store{
		logger: logger.With(attr.SlogComponent("deviceintegrations.store")),
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
	}
}

// Config is the decoded configuration for one org+provider integration.
// Credentials are omitted: callers that need them use
// LoadConfigWithCredentials explicitly so the secret's blast radius stays
// visible at call sites.
type Config struct {
	ID             uuid.UUID
	OrganizationID string
	Provider       string
	Settings       providers.Settings
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func configFromRow(row repo.DeviceIntegrationConfig) (Config, error) {
	settings := providers.Settings{}
	if len(row.Settings) > 0 {
		if err := json.Unmarshal(row.Settings, &settings); err != nil {
			return Config{}, oops.E(oops.CodeUnexpected, err, "decode device integration settings")
		}
	}
	return Config{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Provider:       row.Provider,
		Settings:       settings,
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

// LoadConfig returns the live config for an org+provider, or (nil, nil) when
// none is set.
func (s *Store) LoadConfig(ctx context.Context, orgID string, provider string) (*Config, error) {
	row, err := s.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{
		OrganizationID: orgID,
		Provider:       provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load device integration config")
	}
	cfg, err := configFromRow(row)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfigWithCredentials returns the live config together with its
// decrypted secret material in one read, so callers never observe a config
// and credentials from two different row versions. It errors (rather than
// returning nil) when no config is set, since callers that need credentials
// cannot proceed without one.
func (s *Store) LoadConfigWithCredentials(ctx context.Context, orgID string, provider string) (Config, providers.Credentials, error) {
	row, err := s.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{
		OrganizationID: orgID,
		Provider:       provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Config{}, nil, oops.E(oops.CodeNotFound, err, "no %s integration configured", provider)
		}
		return Config{}, nil, oops.E(oops.CodeUnexpected, err, "load device integration config")
	}
	cfg, err := configFromRow(row)
	if err != nil {
		return Config{}, nil, err
	}
	creds, err := s.decryptCredentials(row.CredentialsEncrypted)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, creds, nil
}

func (s *Store) encryptCredentials(creds providers.Credentials) (string, error) {
	doc, err := json.Marshal(creds)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "encode device integration credentials")
	}
	ct, err := s.enc.Encrypt(doc)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "encrypt device integration credentials")
	}
	return ct, nil
}

func (s *Store) decryptCredentials(stored string) (providers.Credentials, error) {
	plaintext, err := s.enc.Decrypt(stored)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decrypt device integration credentials")
	}
	creds := providers.Credentials{}
	if err := json.Unmarshal([]byte(plaintext), &creds); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode device integration credentials")
	}
	return creds, nil
}

// PreValidateSupplied rejects malformed request shapes — unknown keys and
// under-length secrets — before any transaction or lock is taken, so a bad
// API request never contends with real config saves. Full validation
// (required fields against the merged document) still runs inside the
// upsert transaction.
func PreValidateSupplied(desc providers.Descriptor, creds providers.Credentials, settings providers.Settings) error {
	known := make(map[string]providers.CredentialField, len(desc.Fields))
	for _, f := range desc.Fields {
		known[f.Key] = f
	}
	for key, value := range creds {
		f, ok := known[key]
		if !ok || !f.Secret {
			return oops.E(oops.CodeInvalid, nil, "unknown credential field %q for provider %s", key, desc.ID)
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" && len(trimmed) < minSecretLength {
			return oops.E(oops.CodeInvalid, nil, "%s must be at least %d characters", key, minSecretLength)
		}
	}
	for key := range settings {
		f, ok := known[key]
		if !ok || f.Secret {
			return oops.E(oops.CodeInvalid, nil, "unknown settings field %q for provider %s", key, desc.ID)
		}
	}
	return nil
}

// validateFields checks supplied credential/settings values against the
// descriptor's field spec: required fields present (unless the caller is
// keeping existing credentials), no unknown keys, no blank values.
func validateFields(desc providers.Descriptor, creds providers.Credentials, settings providers.Settings, requireSecrets bool) error {
	known := make(map[string]providers.CredentialField, len(desc.Fields))
	for _, f := range desc.Fields {
		known[f.Key] = f
	}
	for key, value := range creds {
		f, ok := known[key]
		if !ok || !f.Secret {
			return oops.E(oops.CodeInvalid, nil, "unknown credential field %q for provider %s", key, desc.ID)
		}
		// Minimum secret length guarantees the error-scrubbing threshold
		// always covers stored values; real vendor secrets are far longer.
		if trimmed := strings.TrimSpace(value); trimmed != "" && len(trimmed) < minSecretLength {
			return oops.E(oops.CodeInvalid, nil, "%s must be at least %d characters", key, minSecretLength)
		}
	}
	for key := range settings {
		f, ok := known[key]
		if !ok || f.Secret {
			return oops.E(oops.CodeInvalid, nil, "unknown settings field %q for provider %s", key, desc.ID)
		}
	}
	for _, f := range desc.Fields {
		var value string
		var supplied bool
		if f.Secret {
			value, supplied = creds[f.Key]
			if !requireSecrets && !supplied {
				continue
			}
		} else {
			value, supplied = settings[f.Key]
		}
		if f.Required && (!supplied || strings.TrimSpace(value) == "") {
			return oops.E(oops.CodeInvalid, nil, "%s is required for provider %s", f.Key, desc.ID)
		}
		// URL-kind fields get a save-time syntax check so a scheme-less or
		// http URL is rejected at the sheet instead of surfacing an hour
		// later as a deterministic sync failure. Providers still enforce
		// their own stricter shape (tenant root, no path) at request time.
		if f.Kind == providers.FieldKindURL && supplied && strings.TrimSpace(value) != "" {
			parsed, err := url.Parse(strings.TrimSpace(value))
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
				return oops.E(oops.CodeInvalid, nil, "%s must be a valid https URL", f.Key)
			}
		}
	}
	return nil
}

// UpsertResult carries the saved config, whether this call created the
// config row, and the pre-upsert state read inside the same transaction —
// the audit before-snapshot must describe the row the transaction actually
// mutated, not a racy pool-side read.
type UpsertResult struct {
	Config  Config
	Created bool
	// Before is the config state the transaction observed prior to the
	// write; nil when the call created the config.
	Before *Config
	// SyncsMadeDue reports that this save left the config's schedules due to
	// run now (creation seeds them due, a credential/settings change resets
	// them, and an enable transition marks them due) — the caller's signal
	// to kick the sync machinery instead of waiting for its next tick.
	SyncsMadeDue bool
}

// upsertWithTx creates or updates the org+provider config and ensures a
// schedule row plus a sync row exist for each of the provider's declared
// schedules. Credential rotation updates the existing row in place — never a
// soft-delete-and-recreate — because mdm_devices hang off the config id and
// rotating a secret must not orphan the synced fleet inventory. Saving is
// also the user's "try again" signal: machine-initiated auto-pauses are
// lifted, while user-initiated schedule disables are deliberately untouched.
// ProvisionFunc creates the provider's vendor-side object (e.g. a Drata Custom
// Connection) and returns settings to persist. It is invoked inside the upsert
// — after the effective credentials/settings are merged from stored state, and
// while the (org, provider) advisory lock is held — so it sees a complete
// config even on a partial update, and concurrent first-time connects cannot
// race to create duplicate vendor objects. Nil when the caller has nothing to
// provision.
type ProvisionFunc func(context.Context, providers.Credentials, providers.Settings) (providers.Settings, error)

func (s *Store) upsertWithTx(ctx context.Context, dbtx repo.DBTX, desc providers.Descriptor, orgID string, creds providers.Credentials, settings providers.Settings, enabled bool, provision ProvisionFunc) (UpsertResult, error) {
	q := repo.New(dbtx)

	// Serialize concurrent upserts: the advisory lock covers the absent-row
	// case (two first-time saves must not race to a unique violation), and
	// FOR UPDATE covers existing rows so the settings merge cannot read a
	// snapshot replaced beneath it.
	if err := q.AcquireConfigUpsertLock(ctx, repo.AcquireConfigUpsertLockParams{
		OrganizationID: orgID,
		Provider:       desc.ID,
	}); err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "serialize device integration upsert")
	}
	existingRow, err := q.GetConfigByOrgAndProviderForUpdate(ctx, repo.GetConfigByOrgAndProviderForUpdateParams{
		OrganizationID: orgID,
		Provider:       desc.ID,
	})
	exists := true
	var before *Config
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "load device integration config")
		}
		exists = false
	} else {
		cfg, err := configFromRow(existingRow)
		if err != nil {
			return UpsertResult{}, err
		}
		before = &cfg
	}

	credentialsSupplied := len(creds) > 0

	// Settings merge from the stored state: a key the client omits keeps its
	// stored value (so credential rotations and partial updates cannot wipe
	// optional settings); sending a key overwrites it. Clearing a value is an
	// explicit empty string. The merge runs before validation so a partial
	// update is validated as the effective document, not the sparse payload.
	if before != nil {
		merged := providers.Settings{}
		maps.Copy(merged, before.Settings)
		maps.Copy(merged, settings)
		settings = merged
	}

	// Credentials merge the same way: a secret the client omits (or sends
	// blank) keeps its stored value, so rotating one secret never requires
	// retyping the rest — matching the dashboard's "•••••• (saved)"
	// placeholders. Merged values are rebuilt from the descriptor's secret
	// fields, so keys a provider no longer declares fall away on rotation.
	// Secrets cannot be cleared individually; deleting the config is the way
	// to revoke credentials.
	if exists && credentialsSupplied {
		merged := providers.Credentials{}
		if stored, decErr := s.decryptCredentials(existingRow.CredentialsEncrypted); decErr == nil {
			for _, f := range desc.SecretFields() {
				if v, ok := stored[f.Key]; ok {
					merged[f.Key] = v
				}
			}
		}
		// On decrypt failure the stored side contributes nothing: a freshly
		// supplied full set of secrets can still repair a corrupt blob, and a
		// partial set fails required-field validation below.
		for key, value := range creds {
			if strings.TrimSpace(value) != "" {
				merged[key] = value
			}
		}
		creds = merged
	}

	if err := validateFields(desc, creds, settings, !exists || credentialsSupplied); err != nil {
		return UpsertResult{}, err
	}
	// Credentials are only mandatory on create when the provider actually
	// declares secret fields; a secretless provider is legitimate.
	if !exists && !credentialsSupplied && len(desc.SecretFields()) > 0 {
		return UpsertResult{}, oops.E(oops.CodeInvalid, nil, "credentials are required to connect %s", desc.ID)
	}

	// Provision the vendor-side object now — on the merged effective config, so
	// a partial update (a stored key/connection id the client omitted) still
	// provisions correctly, and under the advisory lock, so two first-time
	// connects can't both create a duplicate. Runs only when there's something
	// to provision (nil callback, or the provider no-ops when already set).
	if provision != nil {
		// The settings merge above already made stored settings effective. The
		// credentials merge only ran when the client supplied secrets, so on a
		// settings-only update decrypt the stored credentials for provisioning
		// too — without them a save that still needs a connection would fail
		// with the secret sitting right there in the row.
		provisionCreds := creds
		if len(provisionCreds) == 0 && exists {
			if stored, decErr := s.decryptCredentials(existingRow.CredentialsEncrypted); decErr == nil {
				provisionCreds = stored
			}
		}
		provisioned, err := provision(ctx, provisionCreds, settings)
		if err != nil {
			return UpsertResult{}, err
		}
		settings = provisioned
	}

	settingsDoc, err := json.Marshal(settings)
	if err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "encode device integration settings")
	}

	var row repo.DeviceIntegrationConfig
	switch {
	case !exists:
		encrypted, err := s.encryptCredentials(creds)
		if err != nil {
			return UpsertResult{}, err
		}
		row, err = q.InsertConfig(ctx, repo.InsertConfigParams{
			OrganizationID:       orgID,
			Provider:             desc.ID,
			CredentialsEncrypted: encrypted,
			Settings:             settingsDoc,
			Enabled:              enabled,
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "save device integration config")
		}
	case credentialsSupplied:
		encrypted, err := s.encryptCredentials(creds)
		if err != nil {
			return UpsertResult{}, err
		}
		row, err = q.UpdateConfigCredentials(ctx, repo.UpdateConfigCredentialsParams{
			OrganizationID:       orgID,
			Provider:             desc.ID,
			CredentialsEncrypted: encrypted,
			Settings:             settingsDoc,
			Enabled:              enabled,
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "rotate device integration credentials")
		}
	default:
		row, err = q.UpdateConfigSettings(ctx, repo.UpdateConfigSettingsParams{
			OrganizationID: orgID,
			Provider:       desc.ID,
			Settings:       settingsDoc,
			Enabled:        enabled,
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "save device integration config")
		}
	}

	if err := s.ensureSchedules(ctx, q, desc, row.ID); err != nil {
		return UpsertResult{}, err
	}
	syncsMadeDue := !exists // creation seeds every schedule due
	// A rotated credential or a changed setting is a fresh start: stale
	// failure state must not keep rendering "failed" after the fix, and
	// last_push_digest must not short-circuit the first push to a newly
	// pointed-at account. Settings count because a push destination (e.g.
	// an evidence sink's connection id) is a non-secret setting and the
	// dashboard's write-only secret flow repoints it without resupplying
	// credentials; the coverage digest hashes only the fleet, so without
	// this reset a repointed account would receive nothing until the fleet
	// itself changed.
	settingsChanged := before != nil && !maps.Equal(before.Settings, settings)
	if exists && (credentialsSupplied || settingsChanged) {
		if err := q.ResetSyncStateForConfig(ctx, row.ID); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "reset device integration sync state")
		}
		syncsMadeDue = true
	} else if err := q.ClearAutoPauses(ctx, row.ID); err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "clear device integration sync pauses")
	}

	// An enable transition means "sync now", not "sync whenever the old
	// next_poll_after comes around": a config re-enabled mid-interval would
	// otherwise sit idle for the rest of the hour. Failure history and the
	// push digest are deliberately untouched.
	if exists && enabled && before != nil && !before.Enabled && !syncsMadeDue {
		if err := q.MarkConfigSyncsDue(ctx, row.ID); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "mark device integration syncs due")
		}
		syncsMadeDue = true
	}

	cfg, err := configFromRow(row)
	if err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{Config: cfg, Created: !exists, Before: before, SyncsMadeDue: syncsMadeDue}, nil
}

// ensureSchedules materializes a schedule row and its 1:1 sync row for every
// schedule the provider declares. Existing rows — including their disabled_at
// user intent and sync progress — are left untouched.
func (s *Store) ensureSchedules(ctx context.Context, q *repo.Queries, desc providers.Descriptor, configID uuid.UUID) error {
	for _, spec := range desc.Schedules {
		sched, err := q.EnsureSchedule(ctx, repo.EnsureScheduleParams{
			DeviceIntegrationConfigID: configID,
			Schedule:                  spec.Schedule,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "save device integration schedule")
		}
		if err := q.EnsureSync(ctx, sched.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "save device integration sync")
		}
	}
	return nil
}

func (s *Store) softDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string) error {
	if err := repo.New(dbtx).SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
		OrganizationID: orgID,
		Provider:       provider,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete device integration config")
	}
	return nil
}
