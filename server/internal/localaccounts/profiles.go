// Package localaccounts contains opt-in development account fixtures. It is not
// a production billing or trial lifecycle API.
package localaccounts

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

//go:embed schema.sql
var localSchema string

type Profile string

const (
	Enterprise   Profile = "enterprise"
	PAYG         Profile = "payg"
	ActiveTrial  Profile = "active-trial"
	ExpiredTrial Profile = "expired-trial"
)

func (p Profile) Validate() error {
	switch p {
	case Enterprise, PAYG, ActiveTrial, ExpiredTrial:
		return nil
	}
	return errors.New("profile must be enterprise, payg, active-trial, or expired-trial")
}

type State struct {
	// AccountType is the organization's stored billing tier.
	AccountType string `json:"account_type"`

	// Whitelisted reports whether the organization is allowed access.
	Whitelisted bool `json:"whitelisted"`

	// Trial contains the stored trial lifecycle, or JSON null when absent.
	Trial json.RawMessage `json:"trial"`

	// Features lists the organization's enabled feature flags.
	Features []string `json:"features"`

	// Profile is the local fixture marker, empty when no marker exists.
	Profile Profile `json:"profile,omitempty"`

	// Anchor is the stored fixture's time origin, nil when no marker exists.
	Anchor *time.Time `json:"anchor,omitempty"`
}

type Result struct {
	// Prerequisites lists the local safety checks satisfied before application.
	Prerequisites []string `json:"prerequisites,omitempty"`

	// Before is the account state read before any profile changes.
	Before State `json:"before"`

	// Profile is the requested local account profile.
	Profile Profile `json:"requested_profile,omitempty"`

	// Anchor is the fixture's UTC time origin, reused when reapplying a profile.
	Anchor time.Time `json:"anchor"`

	// DryRun reports whether changes were previewed without writing them.
	DryRun bool `json:"dry_run"`

	// Committed reports whether the database transaction committed, even if cache refresh failed.
	Committed bool `json:"committed"`

	// CacheRefreshError records post-commit cache failures; it does not imply database rollback.
	CacheRefreshError string `json:"cache_refresh_error,omitempty"`
}

// Only the explicit bundle and runtime gates belong to this tool. In particular
// Skills grants, roles, key disable causes, sessions, and resources are untouched.
func profileFeatures() []productfeatures.Feature {
	return slices.Concat(productfeatures.EnterpriseAccessBundle, productfeatures.TrialRuntimeFeatures)
}

func readState(ctx context.Context, db repo.DBTX, orgID string) (State, error) {
	q := repo.New(db)
	row, err := q.ReadAccountState(ctx, orgID)
	if err != nil {
		return State{}, fmt.Errorf("read selected organization state: %w", err)
	}
	s := State{AccountType: row.GramAccountType, Whitelisted: row.Whitelisted, Trial: row.Trial, Features: row.Features, Profile: "", Anchor: nil}
	exists, err := q.AccountProfilesExist(ctx)
	if err != nil {
		return s, fmt.Errorf("check local account schema: %w", err)
	}
	if exists {
		marker, err := q.GetAccountProfile(ctx, conv.ToPGText(orgID))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return s, fmt.Errorf("read local account marker: %w", err)
		}
		if err == nil {
			if !marker.Anchor.Valid || marker.Anchor.InfinityModifier != pgtype.Finite {
				return s, errors.New("invalid local account profile anchor")
			}
			s.Profile = Profile(marker.Profile)
			s.Anchor = &marker.Anchor.Time
		}
	}
	return s, nil
}

// Apply must only be called after local target validation and workflow quiescence.
// Recheck is run inside the transaction before any write to catch changed selection
// or membership. The command supplies a fresh selected-identity resolution.
func Apply(ctx context.Context, db *pgxpool.Pool, features *productfeatures.Client, orgID string, profile Profile, dryRun bool, now time.Time, recheck func(context.Context, pgx.Tx) error) (Result, error) {
	var result Result
	result.Profile = profile
	result.DryRun = dryRun
	if err := profile.Validate(); err != nil {
		return result, err
	}
	if recheck == nil {
		return result, errors.New("selected identity recheck is required")
	}
	conn, release, err := features.AcquireFeatureCacheLocks(ctx, orgID, profileFeatures())
	if err != nil {
		return result, fmt.Errorf("acquire account feature locks: %w", err)
	}
	defer release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin account profile: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	if err = repo.New(tx).LockAccountProfile(ctx); err != nil {
		return result, fmt.Errorf("lock account profile: %w", err)
	}
	if err = recheck(ctx, tx); err != nil {
		return result, err
	}
	// Match trial-start lock ordering: metadata must precede the trial row.
	if err = repo.New(tx).LockAccountMetadata(ctx, orgID); err != nil {
		return result, fmt.Errorf("lock account metadata: %w", err)
	}
	if err = repo.New(tx).LockAccountTrial(ctx, orgID); err != nil {
		return result, fmt.Errorf("lock account trial: %w", err)
	}
	result.Before, err = readState(ctx, tx, orgID)
	if err != nil {
		return result, err
	}
	result.Anchor = now.UTC().Truncate(24 * time.Hour)
	if result.Before.Profile == profile && result.Before.Anchor != nil {
		result.Anchor = result.Before.Anchor.UTC()
	}
	if profile == ActiveTrial && result.Before.Profile == ActiveTrial {
		var trial struct {
			EndsAt    time.Time  `json:"ends_at"`
			DemotedAt *time.Time `json:"demoted_at"`
		}
		if err = json.Unmarshal(result.Before.Trial, &trial); err != nil {
			return result, fmt.Errorf("decode stored trial: %w", err)
		}
		if !result.Anchor.AddDate(0, 0, 14).After(now) || !trial.EndsAt.After(now) || trial.DemotedAt != nil {
			return result, errors.New("stored active-trial fixture has expired or been demoted; apply enterprise then active-trial to start a new fixture")
		}
	}
	if dryRun {
		return result, nil
	}
	if err = applyTx(ctx, tx, orgID, profile, result.Anchor, result.Before); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit account profile: %w", err)
	}
	result.Committed = true
	var cacheErrors []error
	for _, feature := range profileFeatures() {
		if err = features.UpdateFeatureCacheUnderLock(ctx, conn, orgID, feature); err != nil {
			cacheErrors = append(cacheErrors, err)
		}
	}
	if err = errors.Join(cacheErrors...); err != nil {
		result.CacheRefreshError = err.Error()
	}
	return result, nil
}

func Status(ctx context.Context, db *pgxpool.Pool, orgID string) (State, error) {
	return readState(ctx, db, orgID)
}

func applyTx(ctx context.Context, tx pgx.Tx, orgID string, profile Profile, anchor time.Time, before State) error {
	// Deliberately outside public: no production migration, application table,
	// role grant, or demo seed change. This local marker also scopes stub behavior.
	if _, err := tx.Exec(ctx, localSchema); err != nil {
		return fmt.Errorf("create local account schema: %w", err)
	}
	var err error

	tier, whitelisted := string(profile), true
	switch profile {
	case Enterprise, PAYG:
		// These profiles already name their account tier.
	case ActiveTrial:
		tier = "enterprise"
	case ExpiredTrial:
		tier = "free"
		whitelisted = false
	}
	if err = repo.New(tx).SetAccountTier(ctx, repo.SetAccountTierParams{ID: orgID, GramAccountType: tier, Whitelisted: whitelisted}); err != nil {
		return fmt.Errorf("set account tier: %w", err)
	}
	if profile == Enterprise || profile == PAYG {
		if err = repo.New(tx).ConvertAccountTrial(ctx, repo.ConvertAccountTrialParams{OrganizationID: orgID, ConvertedAt: conv.ToPGTimestamptz(anchor)}); err != nil {
			return fmt.Errorf("convert account trial: %w", err)
		}
	} else {
		end := anchor.AddDate(0, 0, 14)
		var demoted *time.Time
		if profile == ExpiredTrial {
			end = anchor.AddDate(0, 0, -1)
			demoted = &anchor
		}
		if err = repo.New(tx).UpsertAccountTrial(ctx, repo.UpsertAccountTrialParams{OrganizationID: orgID, EndsAt: conv.ToPGTimestamptz(end), DemotedAt: conv.PtrToPGTimestamptz(demoted), CreatedAt: conv.ToPGTimestamptz(anchor)}); err != nil {
			return fmt.Errorf("upsert account trial: %w", err)
		}
	}
	q := featurerepo.New(tx)
	// Match paid activation's never-configured policy, not an invented enable-all.
	// Unlike that production helper, deliberately do not provision Skills roles.
	if profile == Enterprise || profile == ActiveTrial || (profile == PAYG && string(before.Trial) == "null") {
		for _, feature := range productfeatures.EnterpriseAccessBundle {
			if profile == PAYG {
				_, err = q.EnableFeatureIfNeverConfigured(ctx, featurerepo.EnableFeatureIfNeverConfiguredParams{OrganizationID: orgID, FeatureName: string(feature)})
			} else {
				_, err = q.EnableFeature(ctx, featurerepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(feature)})
			}
			if err != nil {
				return fmt.Errorf("enable account access feature: %w", err)
			}
		}
	}
	if profile == PAYG && string(before.Trial) == "null" {
		// Never-trialled PAYG activation preserves explicit runtime disables too.
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			if _, err = q.EnableFeatureIfNeverConfigured(ctx, featurerepo.EnableFeatureIfNeverConfiguredParams{OrganizationID: orgID, FeatureName: string(feature)}); err != nil {
				return fmt.Errorf("enable account runtime feature: %w", err)
			}
		}
	} else if err = productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, orgID, profile != ExpiredTrial); err != nil {
		return fmt.Errorf("set account runtime features: %w", err)
	}
	err = repo.New(tx).UpsertAccountProfile(ctx, repo.UpsertAccountProfileParams{OrganizationID: conv.ToPGText(orgID), Profile: string(profile), Anchor: conv.ToPGTimestamptz(anchor)})
	if err != nil {
		return fmt.Errorf("save account profile marker: %w", err)
	}
	return nil
}
