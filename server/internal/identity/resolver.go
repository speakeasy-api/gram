package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	workosRepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// Kind classifies what sort of subject an identity describes. The identity
// page renders the same sections for every kind; the kind decides which of
// them can hold data at all.
type Kind string

const (
	// KindHuman is a person: a directory user, or usage that resolves to one.
	KindHuman Kind = "human"

	// KindAPIKey is a subject acting under an API key rather than a session.
	KindAPIKey Kind = "apikey"

	// KindAgent is an agent identity. Reserved: nothing mints one yet.
	KindAgent Kind = "agent"

	// KindUnattributed is usage that carries an identifier matching no
	// directory row — an external user id, or an email nobody in the org
	// owns. It still has telemetry, cost and risk, but no roles or devices.
	KindUnattributed Kind = "unattributed"
)

// ErrAgentUnsupported is returned for agent identity URNs. The URN kind
// parses so links minted today stay valid, but nothing mints agent identities
// yet, so there is nothing to resolve.
var ErrAgentUnsupported = errors.New("agent identities are not supported yet")

// Directory holds the WorkOS Directory Sync attributes for a person. Every
// field is customer-controlled through IdP mappings and may be absent.
type Directory struct {
	// DepartmentName is the directory department attribute.
	DepartmentName string

	// JobTitle is the directory job title attribute.
	JobTitle string

	// EmployeeType is the directory employment type attribute.
	EmployeeType string

	// DivisionName is the directory division attribute.
	DivisionName string

	// CostCenterName is the directory cost centre attribute.
	CostCenterName string

	// Groups are the directory groups the person currently belongs to.
	Groups []string
}

// Identity is one subject with every identifier its data is keyed under, so a
// caller can fan out to each subsystem with the key that subsystem expects.
type Identity struct {
	// Kind classifies the subject.
	Kind Kind

	// CanonicalURN is the identity URN callers should navigate to. It is the
	// user URN whenever the subject owns a Gram user row, so links built from
	// an email and from a user id converge on one URL.
	CanonicalURN urn.Identity

	// UserIDs are the Gram user ids the subject resolves to. Normally one;
	// an email owned by both a directory row and a linked account resolves
	// to both, and the first is the directory owner.
	UserIDs []string

	// Emails are every address the subject is known by — directory email plus
	// linked AI account emails, normalized. This is the set telemetry and
	// cost aggregate over.
	Emails []string

	// ExternalUserIDs are the identifiers agents report for this subject.
	// Risk findings are keyed on these.
	ExternalUserIDs []string

	// WorkosUserID is the WorkOS user id, which RBAC role assignments key on.
	WorkosUserID string

	// DisplayName is the subject's name, falling back to its primary email.
	DisplayName string

	// PhotoURL is the subject's avatar, when the directory supplied one.
	PhotoURL string

	// Directory holds the Directory Sync attributes, empty when the subject
	// has no directory row.
	Directory Directory
}

// GramUserID is the subject's owning Gram user id, or "" when the identity
// resolves to no directory row.
func (i Identity) GramUserID() string {
	if len(i.UserIDs) == 0 {
		return ""
	}
	return i.UserIDs[0]
}

// PrimaryEmail is the subject's directory email, or "" when it has none.
func (i Identity) PrimaryEmail() string {
	if len(i.Emails) == 0 {
		return ""
	}
	return i.Emails[0]
}

// Resolver maps any identity key to the full set of identifiers the subject
// is known by across Gram's subsystems.
type Resolver struct {
	logger *slog.Logger
	users  *usersRepo.Queries
	hooks  *hooksRepo.Queries
	workos *workosRepo.Queries
}

// NewResolver builds a resolver over the given database pool.
func NewResolver(logger *slog.Logger, db *pgxpool.Pool) *Resolver {
	return &Resolver{
		logger: logger,
		users:  usersRepo.New(db),
		hooks:  hooksRepo.New(db),
		workos: workosRepo.New(db),
	}
}

// Subject is the identifier fan-out for one person: the Gram user ids and the
// emails their work can be attributed to.
type Subject struct {
	// UserIDs are the Gram user ids the identifier resolves to.
	UserIDs []string

	// Emails are every address attributable to the same person.
	Emails []string
}

// Expand resolves an email or a Gram user id into every identifier the same
// person's work is recorded under.
//
// Personal accounts are the reason the linked-account lookup is here: their
// provider email is usually not the directory email, so their usage only
// joins to the person through user_accounts.
//
// Ownership comes from the directory, never from telemetry row identity
// (DNO-509).
//
// Best effort: a directory lookup failure falls back to the identifier alone,
// which is the behaviour that predates this expansion.
func (r *Resolver) Expand(ctx context.Context, orgID, identifier string) Subject {
	subject := Subject{UserIDs: nil, Emails: nil}
	if identifier == "" {
		return subject
	}

	if strings.Contains(identifier, "@") {
		subject.Emails = append(subject.Emails, identifier, conv.NormalizeEmail(identifier))

		// Usage from someone with no directory row still aggregates by email,
		// so an email that resolves to nobody is not an error.
		rows, err := r.users.GetConnectedUsersMatchingEmails(ctx, usersRepo.GetConnectedUsersMatchingEmailsParams{
			Emails:         subject.Emails,
			OrganizationID: orgID,
		})
		if err != nil {
			r.logger.WarnContext(ctx, "failed to resolve identity email to org user", attr.SlogError(err))
		}
		if len(rows) == 1 {
			row := rows[0]
			// These rows already carry the directory email, so no lookup by id.
			subject.UserIDs = append(subject.UserIDs, row.ID)
			subject.Emails = append(subject.Emails, row.Email, conv.NormalizeEmail(row.Email))
		}

		// Directory ownership wins. Only reverse-resolve a provider account
		// when the email has no directory row, and only when one owner claims it.
		if err == nil && len(rows) == 0 {
			accounts, err := r.hooks.ListUserAccountsByEmails(ctx, hooksRepo.ListUserAccountsByEmailsParams{
				OrganizationID: orgID,
				Emails:         subject.Emails,
			})
			if err != nil {
				r.logger.WarnContext(ctx, "failed to resolve identity account email to org user", attr.SlogError(err))
			}
			owners := make([]string, 0, len(accounts))
			for _, account := range accounts {
				owners = append(owners, conv.FromPGTextOrEmpty[string](account.UserID))
			}
			owners = dedupeNonEmpty(owners)
			if len(owners) == 1 {
				subject.UserIDs = append(subject.UserIDs, owners[0])
			}
		}

		// A linked account email resolves through user_accounts rather than
		// users; add its owner's directory email before loading the rest of
		// the accounts.
		if len(subject.UserIDs) > 0 {
			rows, err = r.users.GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
				Ids:            subject.UserIDs,
				OrganizationID: orgID,
			})
			if err != nil {
				r.logger.WarnContext(ctx, "failed to resolve identity account owner", attr.SlogError(err))
			}
			for _, row := range rows {
				subject.Emails = append(subject.Emails, row.Email, conv.NormalizeEmail(row.Email))
			}
		}
	} else {
		subject.UserIDs = append(subject.UserIDs, identifier)

		rows, err := r.users.GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
			Ids:            subject.UserIDs,
			OrganizationID: orgID,
		})
		if err != nil {
			r.logger.WarnContext(ctx, "failed to resolve identity user id to org user", attr.SlogError(err))
		}
		for _, row := range rows {
			subject.Emails = append(subject.Emails, row.Email, conv.NormalizeEmail(row.Email))
		}
	}

	if len(subject.UserIDs) > 0 {
		accounts, err := r.hooks.ListUserAccountsByUsers(ctx, hooksRepo.ListUserAccountsByUsersParams{
			OrganizationID: orgID,
			UserIds:        subject.UserIDs,
		})
		if err != nil {
			r.logger.WarnContext(ctx, "failed to load linked accounts for identity", attr.SlogError(err))
		}
		for _, account := range accounts {
			email := conv.FromPGTextOrEmpty[string](account.Email)
			subject.Emails = append(subject.Emails, email, conv.NormalizeEmail(email))
		}
	}

	subject.Emails = dedupeNonEmpty(subject.Emails)
	subject.UserIDs = dedupeNonEmpty(subject.UserIDs)

	return subject
}

// Resolve maps any identity key to the identifiers the subject's data is
// stored under across every subsystem.
//
// An identity always resolves: a key that matches no directory row yields an
// unattributed identity rather than an error, because usage recorded against
// an unknown email or external id is exactly what the caller wants to see.
func (r *Resolver) Resolve(ctx context.Context, orgID string, subjectURN urn.Identity) (Identity, error) {
	if subjectURN.IsZero() {
		return Identity{}, errors.New("identity urn is empty")
	}

	identity := Identity{
		Kind:            KindUnattributed,
		CanonicalURN:    subjectURN,
		UserIDs:         nil,
		Emails:          nil,
		ExternalUserIDs: nil,
		WorkosUserID:    "",
		DisplayName:     "",
		PhotoURL:        "",
		Directory:       Directory{DepartmentName: "", JobTitle: "", EmployeeType: "", DivisionName: "", CostCenterName: "", Groups: nil},
	}

	switch subjectURN.Kind {
	case urn.IdentityKindAgent:
		return Identity{}, ErrAgentUnsupported

	case urn.IdentityKindAPIKey:
		// An API key subject has no directory identity to expand: user
		// sessions are the only place it appears, keyed by the URN itself.
		identity.Kind = KindAPIKey
		identity.DisplayName = subjectURN.ID
		return identity, nil

	case urn.IdentityKindUser, urn.IdentityKindEmail:
		subject := r.Expand(ctx, orgID, subjectURN.ID)
		identity.UserIDs = subject.UserIDs
		identity.Emails = subject.Emails

	case urn.IdentityKindExternal:
		// Agents commonly report the person's email as the external user id,
		// so an address-shaped value gets the same fold as an email key; a
		// non-address value identifies usage we cannot attribute.
		if strings.Contains(subjectURN.ID, "@") {
			subject := r.Expand(ctx, orgID, subjectURN.ID)
			identity.UserIDs = subject.UserIDs
			identity.Emails = subject.Emails
		}
		identity.ExternalUserIDs = append(identity.ExternalUserIDs, subjectURN.ID)

	default:
		return Identity{}, errors.New("unknown identity urn kind")
	}

	// Every address the person is known by is a candidate external user id:
	// agents report whichever address the local tool was configured with.
	identity.ExternalUserIDs = dedupeNonEmpty(append(identity.ExternalUserIDs, identity.Emails...))

	attached := false
	var lookupErr error
	if userID := identity.GramUserID(); userID != "" {
		attached, lookupErr = r.attachUserProfile(ctx, orgID, userID, &identity)
		if !attached && lookupErr == nil {
			// The id matched no org member. Keep the identifiers the usage is
			// recorded under, but do not claim a directory user the caller
			// would then key user-only sections off.
			//
			// Only on a definitive empty result: a failed lookup says nothing
			// about whether the member exists, and dropping their ids would
			// turn a transient database error into a real person rendering as
			// unattributed.
			identity.UserIDs = nil
		}
	}
	if !attached {
		identity.Kind = KindUnattributed
		// Redirecting the canonical URN is also withheld on a lookup failure,
		// for the same reason: it would move a real member's page.
		if email := identity.PrimaryEmail(); email != "" && lookupErr == nil {
			identity.CanonicalURN = urn.NewEmailIdentity(email)
		}
		if identity.DisplayName == "" {
			identity.DisplayName = identity.PrimaryEmail()
		}
		if identity.DisplayName == "" {
			identity.DisplayName = subjectURN.ID
		}
	}

	return identity, nil
}

// attachUserProfile fills in the directory-backed half of an identity. It
// reports whether a directory row was found, and separately whether the lookup
// itself failed — the caller must not treat "the member does not exist" and
// "we could not tell" the same way.
func (r *Resolver) attachUserProfile(ctx context.Context, orgID, userID string, identity *Identity) (bool, error) {
	rows, err := r.users.GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            []string{userID},
		OrganizationID: orgID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load identity user profile", attr.SlogError(err), attr.SlogUserID(userID))
		return false, fmt.Errorf("load identity user profile: %w", err)
	}
	if len(rows) == 0 {
		// The id resolved no org member: usage exists but the person is not
		// (or no longer) in this directory.
		return false, nil
	}

	row := rows[0]
	identity.Kind = KindHuman
	identity.CanonicalURN = urn.NewUserIdentity(row.ID)
	identity.WorkosUserID = conv.FromPGTextOrEmpty[string](row.WorkosID)
	identity.PhotoURL = conv.FromPGTextOrEmpty[string](row.PhotoUrl)
	identity.DisplayName = row.DisplayName
	if identity.DisplayName == "" {
		identity.DisplayName = row.Email
	}

	identity.Directory = r.loadDirectory(ctx, orgID, userID)

	return true, nil
}

// loadDirectory reads the Directory Sync attributes and group memberships for
// a user. Directory Sync is optional, so an absent row is not an error.
func (r *Resolver) loadDirectory(ctx context.Context, orgID, userID string) Directory {
	directory := Directory{DepartmentName: "", JobTitle: "", EmployeeType: "", DivisionName: "", CostCenterName: "", Groups: nil}

	raw, err := r.workos.GetDirectoryUserAttributesByUserID(ctx, workosRepo.GetDirectoryUserAttributesByUserIDParams{
		UserID:         conv.ToPGText(userID),
		OrganizationID: orgID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No Directory Sync for this org, or the user was directory-deleted.
	case err != nil:
		r.logger.WarnContext(ctx, "failed to load directory attributes for identity", attr.SlogError(err), attr.SlogUserID(userID))
	default:
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			r.logger.WarnContext(ctx, "failed to parse directory attributes for identity", attr.SlogError(err), attr.SlogUserID(userID))
		} else {
			// Values come from customer-controlled IdP mappings, so anything
			// that is not a non-empty string is treated as absent.
			directory.DepartmentName = stringAttribute(payload, "department_name")
			directory.JobTitle = stringAttribute(payload, "job_title")
			directory.EmployeeType = stringAttribute(payload, "employee_type")
			directory.DivisionName = stringAttribute(payload, "division_name")
			directory.CostCenterName = stringAttribute(payload, "cost_center_name")
		}
	}

	groups, err := r.workos.ListCurrentDirectoryGroupsByUserID(ctx, workosRepo.ListCurrentDirectoryGroupsByUserIDParams{
		UserID:         conv.ToPGText(userID),
		OrganizationID: orgID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load directory groups for identity", attr.SlogError(err), attr.SlogUserID(userID))
		return directory
	}
	for _, group := range groups {
		directory.Groups = append(directory.Groups, group.Name)
	}

	return directory
}

func stringAttribute(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// dedupeNonEmpty drops blanks and repeats while keeping first-seen order, so
// that the directory identifier stays first and GramUserID and PrimaryEmail
// are deterministic.
//
// Dropping blanks is the load-bearing half: a directory row with no email
// would otherwise put "" in the identity, and matching lower(user_email) = ”
// would sweep in every email-less row in the project — everyone else's hook
// rows.
func dedupeNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
