package admin

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// hooksReleaseFleetWindowDays matches the fleet gauges in hookfleetmetrics.go.
const hooksReleaseFleetWindowDays = int(hookFleetMetricsWindow / (24 * time.Hour))

func (s *Service) GetHooksReleaseProgress(ctx context.Context, _ *gen.GetHooksReleaseProgressPayload) (*gen.AdminHooksReleaseProgress, error) {
	current, err := plugins.CurrentHooksGeneratorVersion()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read hooks generator version").LogError(ctx, s.logger)
	}
	result, err := s.readHooksPublishState(ctx, current)
	if err != nil {
		return nil, err
	}
	if s.hookFleetVersions.Latest != nil {
		result.CurrentBinaryVersion = new(s.hookFleetVersions.Latest.String())
	}
	if s.hookFleet != nil {
		today := time.Now().UTC().Truncate(24 * time.Hour)
		rows, err := s.hookFleet.VersionMix(ctx, today.AddDate(0, 0, -hooksReleaseFleetWindowDays), today)
		if err != nil {
			// The repository counts still help without the fleet, so the
			// page shows them and marks the installs unavailable.
			_ = oops.E(oops.CodeUnexpected, err, "read hook version mix").LogError(ctx, s.logger)
		} else {
			result.Fleet = summarizeHooksReleaseFleet(rows, s.hookFleetVersions)
		}
	}
	return result, nil
}

// readHooksPublishState reads the pins, the stage and the publish state
// from one snapshot, so a stage completing mid-read can't pair the old default
// pin with the completed stage. The transaction ends before the slower fleet
// read.
func (s *Service) readHooksPublishState(ctx context.Context, current int) (*gen.AdminHooksReleaseProgress, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin hooks release progress read").LogError(ctx, s.logger)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	store := hooksrollout.NewStore(tx)
	defaultPin, err := store.DefaultPin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read default hooks rollout pin").LogError(ctx, s.logger)
	}
	overrides, err := store.ListOverrides(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hooks rollout overrides").LogError(ctx, s.logger)
	}
	stage, err := hooksrollout.NewStageStore(tx).Current(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read staged hooks rollout").LogError(ctx, s.logger)
	}
	repos, err := repo.New(tx).AdminListHooksPublishState(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hooks publish state").LogError(ctx, s.logger)
	}
	return summarizeHooksPublishState(repos, current, defaultPin, stage, overrides, s.staffOrg), nil
}

func summarizeHooksPublishState(rows []repo.AdminListHooksPublishStateRow, current int, defaultPin *hooksrollout.Pin, stage *hooksrollout.Stage, overrides []hooksrollout.Override, staffOrg staff.Organization) *gen.AdminHooksReleaseProgress {
	overrideByOrg := make(map[string]*hooksrollout.Pin, len(overrides))
	for _, override := range overrides {
		overrideByOrg[override.OrganizationID] = &override.Pin
	}

	result := &gen.AdminHooksReleaseProgress{
		CurrentVersion:            current,
		CurrentBinaryVersion:      nil,
		Organizations:             0,
		OrganizationsCleared:      0,
		OrganizationsHeld:         0,
		OrganizationsOnLegacyFlag: 0,
		Repos:                     int64(len(rows)),
		ReposOnCurrent:            0,
		RepoVersions:              []*gen.AdminHooksReleaseRepoVersion{},
		Fleet:                     nil,
	}
	currentVersion := strconv.Itoa(current)
	byVersion := map[string]int64{}
	seen := map[string]bool{}
	for _, row := range rows {
		byVersion[row.PublishedHooksVersion]++
		if row.PublishedHooksVersion == currentVersion {
			result.ReposOnCurrent++
		}
		if seen[row.OrganizationID] {
			continue
		}
		seen[row.OrganizationID] = true
		result.Organizations++

		pins := hooksrollout.OrganizationPins{Override: overrideByOrg[row.OrganizationID], Default: defaultPin, Stage: stage}
		pin, _ := pins.Effective(row.OrganizationID)
		switch {
		case staffOrg.Is(row.OrganizationID):
			result.OrganizationsCleared++
		case pin == nil:
			result.OrganizationsOnLegacyFlag++
		case pin.Version >= current:
			result.OrganizationsCleared++
		default:
			result.OrganizationsHeld++
		}
	}

	for version, count := range byVersion {
		result.RepoVersions = append(result.RepoVersions, &gen.AdminHooksReleaseRepoVersion{Version: version, Repos: count})
	}
	slices.SortFunc(result.RepoVersions, func(a, b *gen.AdminHooksReleaseRepoVersion) int {
		na, errA := strconv.Atoi(a.Version)
		nb, errB := strconv.Atoi(b.Version)
		return newestFirst(errA == nil, errB == nil, func() int { return cmp.Compare(nb, na) }, a.Version, b.Version)
	})
	return result
}

func summarizeHooksReleaseFleet(rows []telemetry.HookVersionMixRow, policy hookversions.Policy) *gen.AdminHooksReleaseFleet {
	type key struct {
		version string
		status  hookversions.Status
	}
	counts := map[key]int64{}
	fleet := &gen.AdminHooksReleaseFleet{
		WindowDays:        hooksReleaseFleetWindowDays,
		Installs:          0,
		InstallsOnCurrent: 0,
		Versions:          []*gen.AdminHooksReleaseInstallVersion{},
	}
	for _, row := range rows {
		installs := int64(row.Installs) //nolint:gosec // install counts are far below MaxInt64
		status := policy.Classify(row.BinaryVersion, row.EndpointGeneration)
		counts[key{version: row.BinaryVersion, status: status}] += installs
		fleet.Installs += installs
		if version, err := semver.NewVersion(row.BinaryVersion); err == nil && policy.Latest != nil && version.Equal(policy.Latest) {
			fleet.InstallsOnCurrent += installs
		}
	}
	for k, installs := range counts {
		fleet.Versions = append(fleet.Versions, &gen.AdminHooksReleaseInstallVersion{
			Version:  k.version,
			Status:   string(k.status),
			Installs: installs,
		})
	}
	slices.SortFunc(fleet.Versions, func(a, b *gen.AdminHooksReleaseInstallVersion) int {
		va, errA := semver.NewVersion(a.Version)
		vb, errB := semver.NewVersion(b.Version)
		if c := newestFirst(errA == nil, errB == nil, func() int { return vb.Compare(va) }, a.Version, b.Version); c != 0 {
			return c
		}
		return cmp.Compare(a.Status, b.Status)
	})
	return fleet
}

// newestFirst orders parsed versions newest first via compare, then versions
// that do not parse, including empty ones, last.
func newestFirst(okA, okB bool, compare func() int, a, b string) int {
	switch {
	case okA && okB:
		return compare()
	case okA:
		return -1
	case okB:
		return 1
	default:
		return cmp.Compare(b, a)
	}
}
