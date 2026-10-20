// Package deviceagentversions records which speakeasyd version each device
// agent runs, one Redis hash per organization per UTC day, so staff can see
// the version mix across organizations without a schema change.
//
// The agent service writes on every getPlugins poll; the admin service reads
// the last few days back. Each hash expires after Retention, so nothing needs
// a sweep. Redis is a cache: a flush loses history, which is acceptable for a
// staff rollout view.
package deviceagentversions

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Retention is how long each day's hash lives. It covers the admin grid's
// widest window plus a day of slack.
const Retention = 31 * 24 * time.Hour

const keyPrefix = "device_agent_versions"

// MaxMachineLength and MaxMachinesPerDay bound what one agent credential can
// write. Identities come partly from client-supplied headers, so without them
// a key holder could send unique serials and grow a day's hash without limit.
const (
	MaxMachineLength  = 256
	MaxMachinesPerDay = 20000
)

// userAgentPattern matches the User-Agent speakeasyd sends, such as
// "speakeasyd/0.3.31". Dev builds report a non-semver version and do not
// match, so they stay out of the fleet view.
var userAgentPattern = regexp.MustCompile(`(?:^|\s)speakeasyd/v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)(?:\s|$)`)

// ParseUserAgent returns the speakeasyd version in a User-Agent header.
func ParseUserAgent(userAgent string) (string, bool) {
	match := userAgentPattern.FindStringSubmatch(strings.TrimSpace(userAgent))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func dayKey(day string, organizationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefix, day, organizationID)
}

func orgsKey(day string) string {
	return fmt.Sprintf("%s:%s:orgs", keyPrefix, day)
}

// Store reads and writes the daily version hashes.
type Store struct {
	redis *redis.Client
}

func NewStore(client *redis.Client) *Store {
	return &Store{redis: client}
}

// Record notes that a machine ran version on the UTC day of at. machine is a
// stable identity for one installation, such as its hardware serial, so a
// machine that polls all day counts once and its latest version wins.
//
// An identity longer than MaxMachineLength is ignored, and a day that already
// holds MaxMachinesPerDay machines accepts updates for those machines only.
func (s *Store) Record(ctx context.Context, organizationID, machine, version string, at time.Time) error {
	if machine == "" || len(machine) > MaxMachineLength {
		return nil
	}
	day := at.UTC().Format(time.DateOnly)
	size, err := s.redis.HLen(ctx, dayKey(day, organizationID)).Result()
	if err != nil {
		return fmt.Errorf("size device agent versions: %w", err)
	}
	if size >= MaxMachinesPerDay {
		known, err := s.redis.HExists(ctx, dayKey(day, organizationID), machine).Result()
		if err != nil {
			return fmt.Errorf("check device agent machine: %w", err)
		}
		if !known {
			return nil
		}
	}
	pipe := s.redis.Pipeline()
	pipe.HSet(ctx, dayKey(day, organizationID), machine, version)
	pipe.Expire(ctx, dayKey(day, organizationID), Retention)
	pipe.SAdd(ctx, orgsKey(day), organizationID)
	pipe.Expire(ctx, orgsKey(day), Retention)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record device agent version: %w", err)
	}
	return nil
}

// Row is one (organization, day, version) cell. Version "" is the
// organization-day total.
type Row struct {
	OrganizationID string `json:"organization_id"`
	Day            string `json:"day"`
	Version        string `json:"version"`
	Machines       int64  `json:"machines"`
}

// Read returns the cells for the windowDays UTC days ending on the day of now,
// ordered by organization, day and version.
func (s *Store) Read(ctx context.Context, windowDays int, now time.Time) ([]Row, error) {
	rows := []Row{}
	end := now.UTC()
	for offset := range windowDays {
		day := end.AddDate(0, 0, -offset).Format(time.DateOnly)
		orgs, err := s.redis.SMembers(ctx, orgsKey(day)).Result()
		if err != nil {
			return nil, fmt.Errorf("list device agent version orgs for %s: %w", day, err)
		}
		if len(orgs) == 0 {
			continue
		}
		// One round trip per day, however many organizations polled.
		pipe := s.redis.Pipeline()
		hashes := make([]*redis.MapStringStringCmd, len(orgs))
		for i, organizationID := range orgs {
			hashes[i] = pipe.HGetAll(ctx, dayKey(day, organizationID))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, fmt.Errorf("read device agent versions for %s: %w", day, err)
		}
		for i, organizationID := range orgs {
			machines := hashes[i].Val()
			if len(machines) == 0 {
				continue
			}
			rows = append(rows, dayRows(organizationID, day, machines)...)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OrganizationID != rows[j].OrganizationID {
			return rows[i].OrganizationID < rows[j].OrganizationID
		}
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Version < rows[j].Version
	})
	return rows, nil
}

func dayRows(organizationID, day string, machines map[string]string) []Row {
	byVersion := map[string]int64{}
	for _, version := range machines {
		byVersion[version]++
	}
	rows := []Row{{OrganizationID: organizationID, Day: day, Version: "", Machines: int64(len(machines))}}
	for version, count := range byVersion {
		rows = append(rows, Row{OrganizationID: organizationID, Day: day, Version: version, Machines: count})
	}
	return rows
}
