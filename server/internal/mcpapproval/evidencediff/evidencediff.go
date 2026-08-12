// Package evidencediff compares the permission-relevant slice of two
// evidence documents: the one a decision froze and the one gathered since.
//
// The comparison is deliberately narrow. Tool declarations, package
// versions, repository statistics, popularity, and traffic all move
// constantly and none of them widen what the server is allowed to do — a
// re-review flag that fires on them trains admins to ignore it. What counts
// is the permission surface (OAuth scopes, authority mode, the credentials
// the server demands) and whether anything published now says it is
// vulnerable (advisories). Maintainer counts were considered and rejected:
// the stored count is blind to the takeover it would supposedly catch (a
// swapped maintainer keeps the count constant) while firing on ordinary
// team churn.
//
// A section is compared only when both gathers actually consulted its
// source. Gathers record failures as gaps rather than absences, so a flaky
// registry or a failed OAuth probe leaves the section nil on one side; a
// comparison that treated nil as empty would report every scope as removed
// one day and restored the next. Silence on an un-gathered section is the
// right bias for a loop whose output is an alert.
package evidencediff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
)

// Fields named in FieldChange entries.
const (
	FieldAuthorityMode       = "authority_mode"
	FieldDynamicRegistration = "dynamic_registration"
	FieldKnownAdvisories     = "known_advisories"
)

// FieldChange is one scalar drift, rendered as strings so the UI never
// re-derives formatting from a mixed-type union.
type FieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// AdvisoryChange is one advisory present now that the decision's snapshot
// did not carry.
type AdvisoryChange struct {
	ID       string `json:"id"`
	Summary  string `json:"summary,omitempty"`
	Severity string `json:"severity,omitempty"`
}

// Diff is what moved between two gathers, restricted to the
// permission-relevant slice.
type Diff struct {
	// Changed reports whether anything below is non-empty.
	Changed bool `json:"changed"`

	// ScopesAdded and ScopesRemoved are OAuth scopes the server's published
	// authority metadata gained or lost since the decision.
	ScopesAdded   []string `json:"scopes_added,omitempty"`
	ScopesRemoved []string `json:"scopes_removed,omitempty"`

	// SecretsAdded and SecretsRemoved are credentials the server demands
	// (required secrets only — an optional credential gates nothing).
	SecretsAdded   []string `json:"secrets_added,omitempty"`
	SecretsRemoved []string `json:"secrets_removed,omitempty"`

	// Fields are scalar drifts: authority mode, dynamic client
	// registration, and the published-advisory count.
	Fields []FieldChange `json:"fields,omitempty"`

	// AdvisoriesAdded are advisories in the current gather's most-recent
	// sample that the snapshot's sample did not carry. There is no removed
	// counterpart: the stored sample is a most-recent window, so an id
	// leaving it usually means newer advisories pushed it out, not that it
	// was withdrawn.
	AdvisoriesAdded []AdvisoryChange `json:"advisories_added,omitempty"`
}

// subset is the canonical projection a fingerprint hashes. Gathered flags
// keep "source not consulted" distinct from "source answered with nothing",
// mirroring the compare-only-when-both-gathered rule.
type subset struct {
	AuthorityGathered   bool     `json:"authority_gathered"`
	AuthorityMode       string   `json:"authority_mode"`
	Scopes              []string `json:"scopes"`
	DynamicRegistration bool     `json:"dynamic_registration"`
	DemandedSecrets     []string `json:"demanded_secrets"`

	AdvisoriesGathered bool     `json:"advisories_gathered"`
	KnownAdvisories    int      `json:"known_advisories"`
	AdvisoryIDs        []string `json:"advisory_ids"`
}

func project(document evidence.Document) subset {
	s := subset{
		AuthorityGathered:   false,
		AuthorityMode:       "",
		Scopes:              []string{},
		DynamicRegistration: false,
		DemandedSecrets:     []string{},
		AdvisoriesGathered:  false,
		KnownAdvisories:     0,
		AdvisoryIDs:         []string{},
	}

	if authority := document.Authority; authority != nil {
		s.AuthorityGathered = true
		s.AuthorityMode = authority.Mode
		s.Scopes = append(s.Scopes, authority.Scopes...)
		slices.Sort(s.Scopes)
		s.DynamicRegistration = authority.DynamicRegistration
		for _, secret := range authority.DemandedSecrets {
			s.DemandedSecrets = append(s.DemandedSecrets, secret.Name)
		}
		slices.Sort(s.DemandedSecrets)
	}

	if advisories := document.Advisories; advisories != nil {
		s.AdvisoriesGathered = true
		s.KnownAdvisories = advisories.KnownCount
		for _, advisory := range advisories.Advisories {
			s.AdvisoryIDs = append(s.AdvisoryIDs, advisory.ID)
		}
		slices.Sort(s.AdvisoryIDs)
	}

	return s
}

// Fingerprint hashes the permission-relevant projection of a document. Two
// documents with the same fingerprint would produce an empty Compare, so it
// serves as the announce-once key for change notifications.
func Fingerprint(document evidence.Document) string {
	encoded, err := json.Marshal(project(document))
	if err != nil {
		// A struct of strings, bools, ints, and string slices cannot fail to
		// marshal; the error path exists to satisfy the signature.
		panic(fmt.Sprintf("marshal evidence fingerprint subset: %v", err))
	}

	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Compare reports what moved from before (the decision's frozen snapshot) to
// after (the current gather), restricted to the permission-relevant slice.
func Compare(before, after evidence.Document) Diff {
	diff := Diff{
		Changed:         false,
		ScopesAdded:     nil,
		ScopesRemoved:   nil,
		SecretsAdded:    nil,
		SecretsRemoved:  nil,
		Fields:          nil,
		AdvisoriesAdded: nil,
	}

	beforeSubset := project(before)
	afterSubset := project(after)

	if beforeSubset.AuthorityGathered && afterSubset.AuthorityGathered {
		if beforeSubset.AuthorityMode != afterSubset.AuthorityMode {
			diff.Fields = append(diff.Fields, FieldChange{
				Field:  FieldAuthorityMode,
				Before: beforeSubset.AuthorityMode,
				After:  afterSubset.AuthorityMode,
			})
		}
		if beforeSubset.DynamicRegistration != afterSubset.DynamicRegistration {
			diff.Fields = append(diff.Fields, FieldChange{
				Field:  FieldDynamicRegistration,
				Before: strconv.FormatBool(beforeSubset.DynamicRegistration),
				After:  strconv.FormatBool(afterSubset.DynamicRegistration),
			})
		}
		diff.ScopesAdded = missingFrom(afterSubset.Scopes, beforeSubset.Scopes)
		diff.ScopesRemoved = missingFrom(beforeSubset.Scopes, afterSubset.Scopes)
		diff.SecretsAdded = missingFrom(afterSubset.DemandedSecrets, beforeSubset.DemandedSecrets)
		diff.SecretsRemoved = missingFrom(beforeSubset.DemandedSecrets, afterSubset.DemandedSecrets)
	}

	if beforeSubset.AdvisoriesGathered && afterSubset.AdvisoriesGathered {
		if beforeSubset.KnownAdvisories != afterSubset.KnownAdvisories {
			diff.Fields = append(diff.Fields, FieldChange{
				Field:  FieldKnownAdvisories,
				Before: strconv.Itoa(beforeSubset.KnownAdvisories),
				After:  strconv.Itoa(afterSubset.KnownAdvisories),
			})
		}
		known := make(map[string]struct{}, len(beforeSubset.AdvisoryIDs))
		for _, id := range beforeSubset.AdvisoryIDs {
			known[id] = struct{}{}
		}
		if after.Advisories != nil {
			for _, advisory := range after.Advisories.Advisories {
				if _, seen := known[advisory.ID]; seen {
					continue
				}
				diff.AdvisoriesAdded = append(diff.AdvisoriesAdded, AdvisoryChange{
					ID:       advisory.ID,
					Summary:  advisory.Summary,
					Severity: advisory.Severity,
				})
			}
		}
	}

	diff.Changed = len(diff.ScopesAdded)+len(diff.ScopesRemoved)+
		len(diff.SecretsAdded)+len(diff.SecretsRemoved)+
		len(diff.Fields)+len(diff.AdvisoriesAdded) > 0

	return diff
}

// missingFrom returns the members of set absent from other, preserving
// set's order. Inputs arrive sorted from project, so the output is sorted.
func missingFrom(set, other []string) []string {
	var missing []string
	for _, member := range set {
		if !slices.Contains(other, member) {
			missing = append(missing, member)
		}
	}
	return missing
}
