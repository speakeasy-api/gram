package admin

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// HookFleetReader reads one organization's hooks installs. Implemented by
// telemetry.HookFleet.
type HookFleetReader interface {
	HookInstalls(ctx context.Context, organizationID string, since time.Time, limit int) (telemetry.HookInstallPage, error)
}

// SetHookFleet enables getOrganizationHookFleet. A nil reader leaves it unavailable.
func (s *Service) SetHookFleet(reader HookFleetReader, versions hookversions.Policy) {
	s.hookFleet = reader
	s.hookFleetVersions = versions
}

const (
	hookFleetInstallLimit   = 1000
	hookFleetQuietAfterDays = 3
)

func (s *Service) GetOrganizationHookFleet(ctx context.Context, payload *gen.GetOrganizationHookFleetPayload) (*gen.AdminHookFleetResult, error) {
	if s.hookFleet == nil || s.hookFleetVersions.Latest == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "hooks fleet is unavailable")
	}
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -payload.WindowDays)
	page, err := s.hookFleet.HookInstalls(ctx, payload.OrganizationID, since, hookFleetInstallLimit)
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
			if shareable.Code == oops.CodeUnexpected {
				return nil, shareable.LogError(ctx, s.logger)
			}
			return nil, shareable
		}
		return nil, oops.E(oops.CodeUnexpected, err, "read hook installs").LogError(ctx, s.logger)
	}
	result := summarizeHookFleet(page, s.hookFleetVersions, now)
	result.OrganizationID = payload.OrganizationID
	result.WindowDays = payload.WindowDays
	result.Since = since.Format(time.RFC3339)
	return result, nil
}

func summarizeHookFleet(page telemetry.HookInstallPage, versions hookversions.Policy, now time.Time) *gen.AdminHookFleetResult {
	quietBefore := now.AddDate(0, 0, -hookFleetQuietAfterDays)
	result := &gen.AdminHookFleetResult{
		OrganizationID: "",
		WindowDays:     0,
		Since:          "",
		LatestVersion:  versions.Latest.String(),
		MinimumVersion: nil,
		QuietAfterDays: hookFleetQuietAfterDays,
		Truncated:      page.Truncated,
		TotalInstalls:  int64(len(page.Installs)),
		QuietInstalls:  0,
		LegacyEvents:   0,
		Versions:       []*gen.AdminHookFleetVersion{},
		Routes:         []*gen.AdminHookFleetBucket{},
		Generations:    []*gen.AdminHookFleetBucket{},
		Installs:       make([]*gen.AdminHookFleetInstall, 0, len(page.Installs)),
	}
	if versions.Minimum != nil {
		minimum := versions.Minimum.String()
		result.MinimumVersion = &minimum
	}

	byVersion := map[string]*gen.AdminHookFleetVersion{}
	byRoute := map[string]*gen.AdminHookFleetBucket{}
	byGeneration := map[string]*gen.AdminHookFleetBucket{}
	bucket := func(buckets map[string]*gen.AdminHookFleetBucket, value string) *gen.AdminHookFleetBucket {
		b, ok := buckets[value]
		if !ok {
			b = &gen.AdminHookFleetBucket{Value: value, Installs: 0, Events: 0}
			buckets[value] = b
		}
		return b
	}

	for _, install := range page.Installs {
		status := string(versions.Classify(install.BinaryVersion, install.EndpointGeneration))
		quiet := install.LastSeen.Before(quietBefore)
		if quiet {
			result.QuietInstalls++
		}

		versionKey := install.BinaryVersion + "\x00" + status
		v, ok := byVersion[versionKey]
		if !ok {
			v = &gen.AdminHookFleetVersion{Version: install.BinaryVersion, Status: status, Installs: 0}
			byVersion[versionKey] = v
		}
		v.Installs++
		bucket(byRoute, install.InstallRoute).Installs++
		bucket(byGeneration, install.EndpointGeneration).Installs++
		for generation, events := range install.EventsByGeneration {
			bucket(byGeneration, generation).Events += clampCount(events)
			if strings.HasPrefix(generation, "legacy_") {
				result.LegacyEvents += clampCount(events)
			}
		}

		result.Installs = append(result.Installs, &gen.AdminHookFleetInstall{
			ProjectID:          install.ProjectID,
			UserEmail:          install.UserEmail,
			Hostname:           install.Hostname,
			Harness:            install.Harness,
			Version:            install.BinaryVersion,
			VersionStatus:      status,
			EndpointGeneration: install.EndpointGeneration,
			InstallRoute:       install.InstallRoute,
			FirstSeen:          install.FirstSeen.UTC().Format(time.RFC3339),
			LastSeen:           install.LastSeen.UTC().Format(time.RFC3339),
			Events:             clampCount(install.Events),
			Errors:             clampCount(install.Errors),
			Quiet:              quiet,
		})
	}

	for _, v := range byVersion {
		result.Versions = append(result.Versions, v)
	}
	slices.SortFunc(result.Versions, func(a, b *gen.AdminHookFleetVersion) int {
		av, aErr := semver.NewVersion(a.Version)
		bv, bErr := semver.NewVersion(b.Version)
		switch {
		case aErr == nil && bErr == nil:
			return cmp.Or(bv.Compare(av), cmp.Compare(a.Version, b.Version), cmp.Compare(a.Status, b.Status))
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			return cmp.Or(cmp.Compare(a.Version, b.Version), cmp.Compare(a.Status, b.Status))
		}
	})
	result.Routes = sortedHookFleetBuckets(byRoute)
	result.Generations = sortedHookFleetBuckets(byGeneration)
	return result
}

func sortedHookFleetBuckets(buckets map[string]*gen.AdminHookFleetBucket) []*gen.AdminHookFleetBucket {
	out := make([]*gen.AdminHookFleetBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b *gen.AdminHookFleetBucket) int {
		return cmp.Or(cmp.Compare(b.Installs, a.Installs), cmp.Compare(b.Events, a.Events), cmp.Compare(a.Value, b.Value))
	})
	return out
}
