package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// Basis names how a matched Speakeasy user was established. It travels with
// the attribution so a consumer can tell an identity the request proved from
// one the message inherited.
type Basis string

const (
	// BasisUnattributed means no rule produced a Speakeasy user. The address and
	// external id the message carried are still recorded so the usage can be
	// folded later, once the directory learns who owns them.
	BasisUnattributed Basis = "unattributed"

	// BasisAgent means an agent principal authenticated the request. The agent
	// is the actor; whatever the batch self-reports describes the human whose
	// machine it runs on and is never recorded as the actor.
	BasisAgent Basis = "agent"

	// BasisCredential means the request credential names the user: a user
	// session, an embedded chat session token, or a key one person holds.
	BasisCredential Basis = "credential"

	// BasisReportedEmail means the address the client reported for the actor
	// is owned by exactly one org member.
	BasisReportedEmail Basis = "reported_email"

	// BasisAccountEmail means the AI account's own address is owned by exactly
	// one org member: a team account signed in with the work email.
	BasisAccountEmail Basis = "account_email"

	// BasisSession means an earlier event of the same session had already been
	// attributed to this person.
	BasisSession Basis = "session"

	// BasisDeviceBridge means the employee was learned from an earlier team
	// session on the same device, which is how a personal account whose address
	// nobody in the directory owns still reaches its employee.
	BasisDeviceBridge Basis = "device_bridge"

	// BasisLinkedAccount means the address belongs to an AI account that one
	// org member linked.
	BasisLinkedAccount Basis = "linked_account"

	// BasisConversation means the chat the message continues already names its
	// owner and the message carried no actor of its own.
	BasisConversation Basis = "conversation"
)

// Credential is what the validated request credential proves about the
// caller. Producers fill it from the auth context, never from the payload.
type Credential struct {
	// Principal is the authenticated principal type, or "" when the request
	// carried no validated principal.
	Principal urn.PrincipalType

	// UserID is the Speakeasy user the credential names, when it names one.
	UserID string

	// Email is the address of the user the credential names.
	Email string

	// ExternalUserID is the end user an embedded chat session token names. It
	// is credential-proven, so it outranks anything the payload reports.
	ExternalUserID string

	// Shared marks a credential many people send under, such as an org-wide
	// plugin hooks key. Its owner, the admin who published it, is never used
	// as a fallback actor: that would credit every machine to the publisher.
	Shared bool

	// SessionIdentityFallback allows an earlier event of the same session to
	// supply the actor when this event carries none. Proxy observations keep
	// it off so a proxied row never inherits an identity the proxy did not see.
	SessionIdentityFallback bool
}

// Reported is what the client payload itself claims about the actor.
type Reported struct {
	// UserEmail is the address the client reported for the human behind the
	// event: a hook payload's user_email, an import's actor email.
	UserEmail string

	// ExternalUserID is the provider's own identifier for the actor, such as a
	// compliance feed's user id. It is recorded as the external user id ahead
	// of the address when present.
	ExternalUserID string
}

// Account is the AI provider account the session runs under, as the provider
// reports it. It is attribution context, never the actor.
type Account struct {
	// Provider is the AI provider tag ("anthropic", "openai", "cursor").
	Provider string

	// DeviceID is the per-device anonymous id the provider reports, stable
	// across the accounts signed in on one machine. It keys the device bridge.
	DeviceID string

	// Email is the account's own report of its address. For a personal account
	// it is usually not the directory address, so it is kept apart from the
	// actor and only resolves when the directory happens to own it.
	Email string
}

// Session is the identity an earlier event of the same session was attributed
// to. The zero value means no earlier event is known.
type Session struct {
	// OrganizationID and ProjectID name the tenant that seeded the entry. The
	// session cache is keyed by a client-reported id alone, so an entry another
	// tenant seeded is never trusted.
	OrganizationID string

	// ProjectID is the project that seeded the entry.
	ProjectID string

	// UserID is the Speakeasy user the session was attributed to.
	UserID string

	// Email is the actor address the session was attributed with.
	Email string
}

// Conversation is the identity already stored on the chat the message
// continues. The zero value means the message opens a new chat.
type Conversation struct {
	// UserID is the Speakeasy user the chat is owned by.
	UserID string

	// ExternalUserID is the external user id the chat was recorded under.
	ExternalUserID string
}

// Signals is everything a producer knows about who is behind one message. A
// user prompt, the assistant reply, a tool call and its result all belong to
// the same session, so a producer matches once per session event and stamps
// the same attribution on every row of the turn.
type Signals struct {
	// OrganizationID scopes every directory lookup.
	OrganizationID string

	// ProjectID is the project the message is written under.
	ProjectID string

	// Credential is what the request credential proves.
	Credential Credential

	// Reported is what the payload claims.
	Reported Reported

	// Account is the AI provider account behind the session.
	Account Account

	// Session is the identity cached for the same session.
	Session Session

	// Conversation is the identity stored on the chat being continued.
	Conversation Conversation
}

// Attribution is the actor identity to record on a message.
type Attribution struct {
	// UserID is the Speakeasy user, or "" when unattributed.
	UserID string

	// Email is the actor's normalized address, or "" when none is known.
	Email string

	// ExternalUserID is the identifier recorded as the message's external user
	// id: the credential's end user, else the provider's actor id, else the
	// actor address, else the id the chat was recorded under.
	ExternalUserID string

	// AccountEmail is the AI account's own normalized address, kept apart from
	// the actor so adopting it never rewrites who the person is.
	AccountEmail string

	// Basis is how UserID was established.
	Basis Basis

	// Ambiguous reports that the actor address is owned by more than one org
	// member. Such an address is attributed to nobody rather than guessed.
	Ambiguous bool

	// LookupFailed reports that a directory lookup errored, so an empty
	// UserID means "could not tell" rather than "nobody". The match is best
	// effort and never blocks a write.
	LookupFailed bool
}

// Authoritative reports whether the Speakeasy user was established from the
// credential or the actor's own resolved address rather than inherited from
// a session, a device, a linked account, or a chat. Only an authoritative
// match may teach the device bridge; an inherited one would teach the bridge
// its own earlier guess.
func (a Attribution) Authoritative() bool {
	switch a.Basis {
	case BasisCredential, BasisReportedEmail, BasisAccountEmail:
		return true
	case BasisUnattributed, BasisAgent, BasisSession, BasisDeviceBridge, BasisLinkedAccount, BasisConversation:
		return false
	default:
		return false
	}
}

// LookupOutcome classifies an address lookup against the directory.
type LookupOutcome string

const (
	// LookupFound means exactly one org member owns the address.
	LookupFound LookupOutcome = "found"

	// LookupNone means no org member owns the address.
	LookupNone LookupOutcome = "none"

	// LookupAmbiguous means several org members own the address, which rows
	// differing only in casing can produce.
	LookupAmbiguous LookupOutcome = "ambiguous"
)

// Lookups are the directory reads the matcher needs. Addresses are passed
// already normalized.
type Lookups interface {
	// UserByEmail resolves an address to the org member that owns it.
	UserByEmail(ctx context.Context, orgID, email string) (string, LookupOutcome, error)

	// DeviceOwner returns the employee linked to a provider device id, or ""
	// when the device is unknown or has no owner yet.
	DeviceOwner(ctx context.Context, orgID, provider, deviceID string) (string, error)

	// AccountOwner returns the single org member who linked an AI account under
	// the address, or "" when none or several did.
	AccountOwner(ctx context.Context, orgID, email string) (string, error)
}

type directoryLookups struct {
	users *usersRepo.Queries
	hooks *hooksRepo.Queries
}

func (d directoryLookups) UserByEmail(ctx context.Context, orgID, email string) (string, LookupOutcome, error) {
	rows, err := d.users.GetConnectedUsersMatchingEmails(ctx, usersRepo.GetConnectedUsersMatchingEmailsParams{
		Emails:         []string{email},
		OrganizationID: orgID,
	})
	if err != nil {
		return "", LookupNone, fmt.Errorf("match org users by email: %w", err)
	}

	switch len(rows) {
	case 0:
		return "", LookupNone, nil
	case 1:
		return rows[0].ID, LookupFound, nil
	default:
		return "", LookupAmbiguous, nil
	}
}

func (d directoryLookups) DeviceOwner(ctx context.Context, orgID, provider, deviceID string) (string, error) {
	owner, err := d.hooks.GetDeviceOwner(ctx, hooksRepo.GetDeviceOwnerParams{
		OrganizationID: orgID,
		Provider:       provider,
		DeviceID:       deviceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get device owner: %w", err)
	}

	return conv.FromPGTextOrEmpty[string](owner.LinkedUserID), nil
}

func (d directoryLookups) AccountOwner(ctx context.Context, orgID, email string) (string, error) {
	accounts, err := d.hooks.ListUserAccountsByEmails(ctx, hooksRepo.ListUserAccountsByEmailsParams{
		OrganizationID: orgID,
		Emails:         []string{email},
	})
	if err != nil {
		return "", fmt.Errorf("list linked accounts by email: %w", err)
	}

	owner, _ := singleAccountOwner(accounts)

	return owner, nil
}

// Matcher attributes a message to the person behind it from the signals its
// producer holds. Every ingestion path (hook adapters, OTEL streams, proxy
// observations, compliance imports, inference hooks, the Elements proxy)
// holds a different subset of the signals, and the one ladder below decides
// the same way for all of them.
//
// The ladder, in order. The first rule that yields a user wins; the address
// and external id are filled independently.
//
//  1. An agent principal is the actor itself: no human identity, no account.
//  2. The actor address is the reported one, else the credential owner's
//     (never for a shared key), else the cached session's (only when fallback
//     is allowed), else the AI account's own.
//  3. The credential names the user when the payload reports no address or
//     reports the owner's own; a shared key only through the second form.
//  4. The actor address resolves to exactly one org member. Several owners
//     make the address ambiguous, and an ambiguous address is nobody's.
//  5. A credential one person holds names that person even when the payload
//     reported an address nobody owns; the owner's address is taken with it so
//     the row and the policy decision name one identity.
//  6. Otherwise the identity is inherited, unless the address was ambiguous:
//     the cached session when it was attributed with the same address, else
//     the device's learned owner, else the single owner of a linked AI account
//     under the address, else the owner of the chat being continued when the
//     message names no other actor.
//
// A resolved identity is never overwritten by a weaker one, and an inherited
// identity never teaches the device bridge.
type Matcher struct {
	logger  *slog.Logger
	lookups Lookups
}

// NewMatcher builds a matcher over the given database pool.
func NewMatcher(logger *slog.Logger, db *pgxpool.Pool) *Matcher {
	return &Matcher{
		logger:  logger,
		lookups: directoryLookups{users: usersRepo.New(db), hooks: hooksRepo.New(db)},
	}
}

// Match attributes one message. It never fails: a lookup error is logged and
// reported on the attribution, and the ladder continues with what it has.
func (m *Matcher) Match(ctx context.Context, signals Signals) Attribution {
	attribution := Attribution{
		UserID:         "",
		Email:          "",
		ExternalUserID: "",
		AccountEmail:   conv.NormalizeEmail(signals.Account.Email),
		Basis:          BasisUnattributed,
		Ambiguous:      false,
		LookupFailed:   false,
	}

	if signals.Credential.Principal == urn.PrincipalTypeAgent {
		attribution.AccountEmail = ""
		attribution.Basis = BasisAgent
		return attribution
	}

	credential := signals.Credential
	tokenEmail := conv.NormalizeEmail(credential.Email)
	reported := conv.NormalizeEmail(signals.Reported.UserEmail)
	session := trustedSession(signals)

	email, emailBasis := actorEmail(reported, tokenEmail, credential, session, attribution.AccountEmail)
	attribution.Email = email

	namesOwner := (reported != "" && reported == tokenEmail) || (reported == "" && !credential.Shared)
	if credential.UserID != "" && namesOwner {
		attribution.UserID = credential.UserID
		attribution.Basis = BasisCredential
	}

	if attribution.UserID == "" && email != "" {
		userID, outcome, err := m.lookups.UserByEmail(ctx, signals.OrganizationID, email)
		switch {
		case err != nil:
			m.logger.WarnContext(ctx, "identity match: resolve actor email", attr.SlogError(err), attr.SlogOrganizationID(signals.OrganizationID))
			attribution.LookupFailed = true
		case outcome == LookupFound:
			attribution.UserID = userID
			attribution.Basis = emailBasis
		case outcome == LookupAmbiguous:
			attribution.Ambiguous = true
		}
	}

	if attribution.UserID == "" && credential.UserID != "" && !credential.Shared {
		attribution.UserID = credential.UserID
		attribution.Basis = BasisCredential
		if tokenEmail != "" {
			attribution.Email = tokenEmail
		}
	}

	if attribution.UserID == "" && !attribution.Ambiguous {
		m.inherit(ctx, signals, session, &attribution)
	}

	attribution.ExternalUserID = externalUserID(signals, attribution.Email)
	if attribution.ExternalUserID == "" {
		attribution.ExternalUserID = strings.TrimSpace(signals.Conversation.ExternalUserID)
	}

	return attribution
}

// inherit applies the rules that borrow an identity established elsewhere,
// in order of how directly the borrowed identity describes this event.
func (m *Matcher) inherit(ctx context.Context, signals Signals, session Session, attribution *Attribution) {
	email := attribution.Email

	// Same session, same person. A session attributed without an address
	// does not lend its user to an event that names one the directory does
	// not know: that address is somebody else until proven otherwise.
	if signals.Credential.SessionIdentityFallback && session.UserID != "" &&
		(email == "" || conv.NormalizeEmail(session.Email) == email) {
		attribution.UserID = session.UserID
		attribution.Basis = BasisSession
		if attribution.Email == "" {
			attribution.Email = conv.NormalizeEmail(session.Email)
		}
		return
	}

	account := signals.Account
	if account.DeviceID != "" {
		owner, err := m.lookups.DeviceOwner(ctx, signals.OrganizationID, account.Provider, account.DeviceID)
		switch {
		case err != nil:
			m.logger.WarnContext(ctx, "identity match: resolve device owner", attr.SlogError(err), attr.SlogOrganizationID(signals.OrganizationID))
			attribution.LookupFailed = true
		case owner != "":
			attribution.UserID = owner
			attribution.Basis = BasisDeviceBridge
			return
		}
	}

	if email != "" {
		owner, err := m.lookups.AccountOwner(ctx, signals.OrganizationID, email)
		switch {
		case err != nil:
			m.logger.WarnContext(ctx, "identity match: resolve linked account owner", attr.SlogError(err), attr.SlogOrganizationID(signals.OrganizationID))
			attribution.LookupFailed = true
		case owner != "":
			attribution.UserID = owner
			attribution.Basis = BasisLinkedAccount
			return
		}
	}

	// The chat's owner continues a chat only when the message names no other
	// actor: the inference path matches a frame to a chat by actor key, so a
	// frame that omits its email still belongs to the chat's owner.
	conversation := signals.Conversation
	stored := strings.TrimSpace(conversation.ExternalUserID)
	incoming := externalUserID(signals, email)
	if conversation.UserID != "" && (incoming == "" || stored == "" || stored == incoming) {
		attribution.UserID = conversation.UserID
		attribution.Basis = BasisConversation
	}
}

// trustedSession returns the cached session identity only when the same
// tenant seeded it.
func trustedSession(signals Signals) Session {
	session := signals.Session
	if session.OrganizationID == "" || session.OrganizationID != signals.OrganizationID || session.ProjectID != signals.ProjectID {
		return Session{OrganizationID: "", ProjectID: "", UserID: "", Email: ""}
	}

	return session
}

// actorEmail picks the address that describes the actor and the basis a
// resolution of it would carry.
func actorEmail(reported, tokenEmail string, credential Credential, session Session, accountEmail string) (string, Basis) {
	switch {
	case reported != "":
		return reported, BasisReportedEmail
	case tokenEmail != "" && !credential.Shared:
		return tokenEmail, BasisCredential
	case credential.SessionIdentityFallback && session.Email != "":
		return conv.NormalizeEmail(session.Email), BasisSession
	case accountEmail != "":
		return accountEmail, BasisAccountEmail
	default:
		return "", BasisUnattributed
	}
}

// externalUserID is the identifier recorded as the message's external user id
// from what this event carries: the credential-proven end user, else the
// provider's actor id, else the actor address.
func externalUserID(signals Signals, email string) string {
	if id := strings.TrimSpace(signals.Credential.ExternalUserID); id != "" {
		return id
	}
	if id := strings.TrimSpace(signals.Reported.ExternalUserID); id != "" {
		return id
	}

	return email
}
