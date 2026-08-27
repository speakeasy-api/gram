package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
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

// Record is one subject with every identifier its data is keyed under, so a
// caller can fan out to each subsystem with the key that subsystem expects.
type Record struct {
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
func (i Record) GramUserID() string {
	if len(i.UserIDs) == 0 {
		return ""
	}
	return i.UserIDs[0]
}

// PrimaryEmail is the subject's directory email, or "" when it has none.
func (i Record) PrimaryEmail() string {
	if len(i.Emails) == 0 {
		return ""
	}
	return i.Emails[0]
}

// Resolver maps any identity key to the full set of identifiers the subject
// is known by across Gram's subsystems.
type Resolver struct {
	logger    *slog.Logger
	users     *usersRepo.Queries
	hooks     *hooksRepo.Queries
	directory *directory.Service
}

// NewResolver builds a resolver over the given database pool.
func NewResolver(logger *slog.Logger, db *pgxpool.Pool) *Resolver {
	return &Resolver{
		logger:    logger,
		users:     usersRepo.New(db),
		hooks:     hooksRepo.New(db),
		directory: directory.NewService(db),
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

// ExpandIdentifier resolves an email or a Gram user id into every identifier
// the same person's work is recorded under.
//
// Personal accounts are the reason the linked-account lookup is here: their
// provider email is usually not the directory email, so their usage only
// joins to the person through user_accounts.
//
// Ownership comes from the directory, never from telemetry row identity
// (DNO-509).
//
// Best effort: a directory lookup failure falls back to the identifier alone,
// which is the behaviour that predates this expansion. An empty identifier is
// the one hard error: an empty subject would widen, not narrow, every filter
// built from it.
func (r *Resolver) ExpandIdentifier(ctx context.Context, orgID, identifier string) (Subject, error) {
	if identifier == "" {
		return Subject{UserIDs: nil, Emails: nil}, errors.New("identity identifier is empty")
	}

	if isEmailIdentifier(identifier) {
		return r.ExpandEmail(ctx, orgID, identifier), nil
	}

	return r.ExpandUserID(ctx, orgID, identifier), nil
}

// ExpandEmail folds an address, and ExpandUserID a Gram user id, onto the same
// set of identifiers. Callers that already know which namespace they hold — an
// identity URN carries its kind — must use these rather than the classifier,
// so a user id shaped like an address cannot resolve against someone else.
func (r *Resolver) ExpandEmail(ctx context.Context, orgID, email string) Subject {
	subject := r.expandEmail(ctx, orgID, email)
	r.completeSubject(ctx, orgID, &subject)

	return subject
}

// ExpandUserID folds a Gram user id onto every identifier the same person's
// work is recorded under.
func (r *Resolver) ExpandUserID(ctx context.Context, orgID, userID string) Subject {
	subject := r.expandUserID(ctx, orgID, userID)
	r.completeSubject(ctx, orgID, &subject)

	return subject
}

// completeSubject adds the identifiers that do not depend on which namespace
// the caller started from, and settles the order both fields promise.
func (r *Resolver) completeSubject(ctx context.Context, orgID string, subject *Subject) {
	r.appendLinkedAccountEmails(ctx, orgID, subject)

	subject.Emails = dedupeNonEmpty(subject.Emails)
	subject.UserIDs = dedupeNonEmpty(subject.UserIDs)
}

// expandEmail folds an address onto the person who owns it. Usage from someone
// with no directory row still aggregates by email, so an address that owns
// nobody is not an error — it is simply an identity with no user ids.
func (r *Resolver) expandEmail(ctx context.Context, orgID, email string) Subject {
	subject := Subject{UserIDs: nil, Emails: []string{email, conv.NormalizeEmail(email)}}

	rows, err := r.users.GetConnectedUsersMatchingEmails(ctx, usersRepo.GetConnectedUsersMatchingEmailsParams{
		Emails:         subject.Emails,
		OrganizationID: orgID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to resolve identity email to org user", attr.SlogError(err))
		return subject
	}

	switch len(rows) {
	case 1:
		// The directory row already carries the canonical email, so there is
		// nothing more to look up by id.
		subject.UserIDs = append(subject.UserIDs, rows[0].ID)
		subject.Emails = append(subject.Emails, rows[0].Email, conv.NormalizeEmail(rows[0].Email))

	case 0:
		// Directory ownership wins, so a provider account is only
		// reverse-resolved when the address has no directory row at all.
		owner, ok := r.accountOwner(ctx, orgID, subject.Emails)
		if !ok {
			break
		}
		subject.UserIDs = append(subject.UserIDs, owner)
		// A linked account email resolves through user_accounts rather than
		// users, so the owner's directory email still has to be added.
		subject.Emails = append(subject.Emails, r.directoryEmails(ctx, orgID, subject.UserIDs)...)

	default:
		// More than one member matches: ownership is ambiguous, so the address
		// stays attached to no user.
	}

	return subject
}

// expandUserID folds a Gram user id onto the addresses their work is recorded
// under.
func (r *Resolver) expandUserID(ctx context.Context, orgID, userID string) Subject {
	return Subject{
		UserIDs: []string{userID},
		Emails:  r.directoryEmails(ctx, orgID, []string{userID}),
	}
}

// accountOwner returns the single Gram user who linked an AI provider account
// under one of these addresses. No owner and several owners are the same
// answer: nobody the usage can be attributed to.
func (r *Resolver) accountOwner(ctx context.Context, orgID string, emails []string) (string, bool) {
	accounts, err := r.hooks.ListUserAccountsByEmails(ctx, hooksRepo.ListUserAccountsByEmailsParams{
		OrganizationID: orgID,
		Emails:         emails,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to resolve identity account email to org user", attr.SlogError(err))
		return "", false
	}

	owners := make([]string, 0, len(accounts))
	for _, account := range accounts {
		owners = append(owners, conv.FromPGTextOrEmpty[string](account.UserID))
	}
	owners = dedupeNonEmpty(owners)
	if len(owners) != 1 {
		return "", false
	}

	return owners[0], true
}

// directoryEmails returns the directory addresses of the given org members,
// both verbatim and normalized.
func (r *Resolver) directoryEmails(ctx context.Context, orgID string, userIDs []string) []string {
	rows, err := r.users.GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            userIDs,
		OrganizationID: orgID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to resolve identity user id to org user", attr.SlogError(err))
		return nil
	}

	emails := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		if row.DeletedAt.Valid {
			continue
		}
		emails = append(emails, row.Email, conv.NormalizeEmail(row.Email))
	}

	return emails
}

// appendLinkedAccountEmails adds the addresses of every AI provider account
// the subject's users have linked.
func (r *Resolver) appendLinkedAccountEmails(ctx context.Context, orgID string, subject *Subject) {
	if len(subject.UserIDs) == 0 {
		return
	}

	accounts, err := r.hooks.ListUserAccountsByUsers(ctx, hooksRepo.ListUserAccountsByUsersParams{
		OrganizationID: orgID,
		UserIds:        subject.UserIDs,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load linked accounts for identity", attr.SlogError(err))
		return
	}

	for _, account := range accounts {
		email := conv.FromPGTextOrEmpty[string](account.Email)
		subject.Emails = append(subject.Emails, email, conv.NormalizeEmail(email))
	}
}

// isEmailIdentifier reports whether an identifier is an address rather than a
// Gram user id. A bare "@" test would classify anything containing one as an
// address and fold it against every email-keyed row, so the value is parsed.
// Only the bare form counts: a display name ("Dev User <dev@example.com>") or a
// quoted local part is not what any subsystem stores an address under, and
// requiring the parse to round-trip rejects both.
func isEmailIdentifier(identifier string) bool {
	address, err := mail.ParseAddress(identifier)
	return err == nil && address.Name == "" && address.Address == identifier
}

// Resolve maps any identity key to the identifiers the subject's data is
// stored under across every subsystem.
//
// An identity always resolves: a key that matches no directory row yields an
// unattributed identity rather than an error, because usage recorded against
// an unknown email or external id is exactly what the caller wants to see.
func (r *Resolver) Resolve(ctx context.Context, orgID string, subjectURN urn.Identity) (Record, error) {
	if subjectURN.IsZero() {
		return Record{}, errors.New("identity urn is empty")
	}

	record := Record{
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
		return Record{}, ErrAgentUnsupported

	case urn.IdentityKindAPIKey:
		// An API key subject has no directory identity to expand: user
		// sessions are the only place it appears, keyed by the URN itself.
		record.Kind = KindAPIKey
		record.DisplayName = subjectURN.ID
		return record, nil

	case urn.IdentityKindUser:
		// The URN carries the namespace, so a user id is expanded as a user id
		// even when it happens to be shaped like an address.
		subject := r.ExpandUserID(ctx, orgID, subjectURN.ID)
		record.UserIDs = subject.UserIDs
		record.Emails = subject.Emails

	case urn.IdentityKindEmail:
		subject := r.ExpandEmail(ctx, orgID, subjectURN.ID)
		record.UserIDs = subject.UserIDs
		record.Emails = subject.Emails

	case urn.IdentityKindExternal:
		// Agents commonly report the person's email as the external user id,
		// so an address gets the same fold as an email key; anything else
		// identifies usage we cannot attribute.
		if isEmailIdentifier(subjectURN.ID) {
			subject := r.ExpandEmail(ctx, orgID, subjectURN.ID)
			record.UserIDs = subject.UserIDs
			record.Emails = subject.Emails
		}
		record.ExternalUserIDs = append(record.ExternalUserIDs, subjectURN.ID)

	default:
		return Record{}, errors.New("unknown identity urn kind")
	}

	// Every address the person is known by is a candidate external user id:
	// agents report whichever address the local tool was configured with.
	record.ExternalUserIDs = dedupeNonEmpty(append(record.ExternalUserIDs, record.Emails...))

	attached := false
	if userID := record.GramUserID(); userID != "" {
		found, err := r.attachUserProfile(ctx, orgID, userID, &record)
		if err != nil {
			// A failed lookup says nothing about whether the member exists.
			// Reporting them as unattributed would turn a transient database
			// error into a real person rendering with no roles and no devices,
			// so the caller is told the identity could not be resolved.
			return Record{}, fmt.Errorf("resolve identity user profile: %w", err)
		}
		if !found {
			// The id matched no org member. Keep the identifiers the usage is
			// recorded under, but do not claim a directory user the caller
			// would then key user-only sections off.
			record.UserIDs = nil
		}
		attached = found
	}
	if !attached {
		record.Kind = KindUnattributed
		if email := record.PrimaryEmail(); email != "" {
			record.CanonicalURN = urn.NewEmailIdentity(email)
		}
		if record.DisplayName == "" {
			record.DisplayName = record.PrimaryEmail()
		}
		if record.DisplayName == "" {
			record.DisplayName = subjectURN.ID
		}
	}

	return record, nil
}

// attachUserProfile fills in the directory-backed half of an record. It
// reports whether a directory row was found, and separately whether the lookup
// itself failed — the caller must not treat "the member does not exist" and
// "we could not tell" the same way.
func (r *Resolver) attachUserProfile(ctx context.Context, orgID, userID string, record *Record) (bool, error) {
	rows, err := r.users.GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            []string{userID},
		OrganizationID: orgID,
	})
	if err != nil {
		r.logger.WarnContext(ctx, "failed to load identity user profile", attr.SlogError(err), attr.SlogUserID(userID))
		return false, fmt.Errorf("load identity user profile: %w", err)
	}
	// GetConnectedUsersByIDs excludes removed memberships but not soft-deleted
	// user rows, so a deleted person would otherwise come back as a member's
	// profile to any org:read caller.
	row, ok := firstActive(rows)
	if !ok {
		// The id resolved no org member: usage exists but the person is not
		// (or no longer) in this directory.
		return false, nil
	}
	record.Kind = KindHuman
	record.CanonicalURN = urn.NewUserIdentity(row.ID)
	record.WorkosUserID = conv.FromPGTextOrEmpty[string](row.WorkosID)
	record.PhotoURL = conv.FromPGTextOrEmpty[string](row.PhotoUrl)
	record.DisplayName = row.DisplayName
	if record.DisplayName == "" {
		record.DisplayName = row.Email
	}

	record.Directory = r.loadDirectory(ctx, orgID, userID)

	return true, nil
}

// loadDirectory reads the Directory Sync attributes and group memberships for
// a user. Directory Sync is optional, so an absent profile is not an error.
func (r *Resolver) loadDirectory(ctx context.Context, orgID, userID string) Directory {
	empty := Directory{DepartmentName: "", JobTitle: "", EmployeeType: "", DivisionName: "", CostCenterName: "", Groups: nil}

	profile, err := r.directory.GetUserProfile(ctx, orgID, userID)
	switch {
	case errors.Is(err, directory.ErrUserNotFound):
		// No Directory Sync for this org, or the user was directory-deleted.
		return empty
	case err != nil:
		r.logger.WarnContext(ctx, "failed to load directory profile for identity", attr.SlogError(err), attr.SlogUserID(userID))
		return empty
	}

	// IdP mappings are customer-controlled, so a value that is only whitespace
	// is reported as absent rather than as a blank attribute.
	attributes := profile.Attributes()

	return Directory{
		DepartmentName: strings.TrimSpace(attributes.DepartmentName),
		JobTitle:       strings.TrimSpace(attributes.JobTitle),
		EmployeeType:   strings.TrimSpace(attributes.EmployeeType),
		DivisionName:   strings.TrimSpace(attributes.DivisionName),
		CostCenterName: strings.TrimSpace(attributes.CostCenterName),
		Groups:         profile.GroupNames(),
	}
}

// firstActive returns the first row that is still an active user. Only the
// membership join is filtered in SQL, so a soft-deleted user row reaches here.
func firstActive(rows []usersRepo.User) (usersRepo.User, bool) {
	for _, row := range rows {
		if !row.DeletedAt.Valid {
			return row, true
		}
	}

	var none usersRepo.User

	return none, false
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
