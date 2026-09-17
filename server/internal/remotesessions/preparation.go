package remotesessions

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const PreparationIDJAGProfile = "urn:ietf:params:oauth:grant-profile:id-jag"

// #nosec G101 -- Public OAuth grant identifier, not a credential.
const PreparationJWTBearerGrant = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// PreparationResourceMetadata contains optional caller-declared consistency hints
// for project-write-authorized configuration, not provider-verified RFC 9728 evidence.
// Preparation does not exchange tokens or establish trust, consent, or user access.
type PreparationResourceMetadata struct {
	Resource             string
	AuthorizationServers []string
}
type PreparationInput struct {
	UserSessionIssuerID     uuid.UUID
	RemoteSessionIssuerID   uuid.UUID
	Resource                string
	ClientID                uuid.UUID
	Scopes                  []string
	Mechanism               string
	ConfirmGrants           []string
	ExpectedGeneration      int64
	TokenEndpointAuthMethod string
	ResourceMetadata        *PreparationResourceMetadata
}

// PreparationResult never carries credentials. Ready means registration recorded,
// not successful token exchange or authorization for any human.
type PreparationResult struct {
	State            string
	Stage            string
	Remediation      string
	Retryable        bool
	BindingID        uuid.UUID
	Generation       int64
	ClientID         uuid.UUID
	ExternalClientID string
	Issuer           string
	Resource         string
	GrantTypes       []string
	Scopes           []string
	GrantSource      string
}

func PreparationEligibility(profiles, grants []string) string {
	if !slices.Contains(profiles, PreparationIDJAGProfile) {
		return "unsupported_profile"
	}
	if !slices.Contains(grants, PreparationJWTBearerGrant) {
		return "incomplete_metadata"
	}
	return "eligible"
}
func preparationDiagnostic(state string) *PreparationResult {
	r := &PreparationResult{State: state, Stage: "selection", GrantSource: "unknown", Remediation: "", Retryable: false, BindingID: uuid.Nil, Generation: 0, ClientID: uuid.Nil, ExternalClientID: "", Issuer: "", Resource: "", GrantTypes: nil, Scopes: nil}
	switch state {
	case "unsupported_profile":
		r.Stage = "eligibility"
		r.Remediation = "The issuer does not advertise the ID-JAG profile. Configure a supported resource authorization server."
	case "incomplete_metadata":
		r.Stage = "eligibility"
		r.Remediation = "Refresh discovery or ask the provider to advertise JWT-bearer support."
	case "unknown_grants":
		r.Stage = "registration"
		r.Remediation = "An administrator must confirm effective registration grants."
	case "manual_setup_required":
		r.Remediation = "Configure a separate downstream client with JWT-bearer and a supported authentication method."
	case "configuration_required":
		r.Remediation = "Select an explicit client or unlink/rebind using the current binding generation."
	case "in_progress":
		r.Stage = "registration"
		r.Retryable = true
		r.Remediation = "Read preparation status; do not submit another registration."
	case "indeterminate":
		r.Stage = "registration"
		r.Remediation = "Reconcile with the provider, then explicitly select the resulting registration or unlink before a new attempt. Do not blindly repeat registration."
	case "provider_rejection":
		r.Stage = "registration"
		r.Remediation = "Review the provider registration requirements before explicitly retrying."
	case "transient_failure":
		r.Stage = "discovery"
		r.Retryable = true
		r.Remediation = "Refresh issuer discovery and retry."
	case "published_acceptance_unverified":
		r.Stage = "publication"
		r.Remediation = "Metadata is published. Provider acceptance and human access remain unverified."
	case "ready":
		r.Stage = "registration"
		r.Remediation = "Registration is recorded. Per-user authorization must be verified by token exchange."
	case "unlinked":
		r.Remediation = "The binding is unlinked and prior generations are invalid."
	}
	return r
}

// Legacy bindings can have NULL preparation fields. Missing state must not
// imply readiness, and missing provenance must not imply verified grants.
func preparationBindingState(state pgtype.Text) string {
	if !state.Valid {
		return "configuration_required"
	}
	return state.String
}

func preparationBindingGrantSource(source pgtype.Text) string {
	if !source.Valid {
		return "unknown"
	}
	return source.String
}

func preparationResult(b repo.RemoteSessionEmaBinding, issuer repo.RemoteSessionIssuer, client repo.RemoteSessionClient, state string) *PreparationResult {
	r := preparationDiagnostic(state)
	r.BindingID = b.ID
	r.Generation = b.Generation
	r.Resource = b.Resource
	r.Issuer = issuer.Issuer
	r.GrantSource = preparationBindingGrantSource(b.GrantSource)
	r.Scopes = b.RequestedScopes
	if b.RemoteSessionClientID.Valid && client.ID == b.RemoteSessionClientID.UUID {
		r.ClientID = b.RemoteSessionClientID.UUID
		r.ExternalClientID = client.ClientID
		r.GrantTypes = client.GrantTypes
	}
	return r
}

// Registration grant records are sets, with NULL distinct from explicit empty.
func samePreparationGrants(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
func normalizePreparationInput(in PreparationInput) (PreparationInput, error) {
	u, err := url.Parse(in.Resource)
	if err != nil || !urls.IsAbsoluteHTTPSOrLoopback(in.Resource) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return in, oops.E(oops.CodeBadRequest, err, "invalid canonical resource")
	}
	// RFC 8707 identifiers are exact: a trailing slash is not discarded.
	if in.UserSessionIssuerID == uuid.Nil || in.RemoteSessionIssuerID == uuid.Nil {
		return in, oops.C(oops.CodeBadRequest)
	}
	scopes := make([]string, 0, len(in.Scopes))
	for _, scope := range in.Scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n\"\\") {
			return in, oops.E(oops.CodeBadRequest, nil, "invalid scope")
		}
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	in.Scopes = slices.Compact(scopes)
	if in.Mechanism != "" && in.Mechanism != "manual" && in.Mechanism != "cimd" && in.Mechanism != "dcr" {
		return in, oops.E(oops.CodeBadRequest, nil, "invalid preparation mechanism")
	}
	for _, g := range in.ConfirmGrants {
		if g == "" || strings.ContainsAny(g, " \t\r\n") {
			return in, oops.E(oops.CodeBadRequest, nil, "invalid grant type")
		}
	}
	in.ConfirmGrants = slices.Clone(in.ConfirmGrants)
	slices.Sort(in.ConfirmGrants)
	in.ConfirmGrants = slices.Compact(in.ConfirmGrants)
	return in, nil
}
func (s *Service) preparationTenant(ctx context.Context, write bool) (uuid.UUID, string, error) {
	a, ok := contextvalues.GetAuthContext(ctx)
	if !ok || a == nil || a.ProjectID == nil {
		return uuid.Nil, "", oops.C(oops.CodeUnauthorized)
	}
	scope := authz.ScopeProjectRead
	if write {
		scope = authz.ScopeProjectWrite
	}
	if err := s.authz.Require(ctx, authz.Check{ResourceKind: "", Dimensions: nil, Scope: scope, ResourceID: a.ProjectID.String()}); err != nil {
		return uuid.Nil, "", err
	}
	return *a.ProjectID, a.ActiveOrganizationID, nil
}
func setPreparationBinding(ctx context.Context, q *repo.Queries, b repo.RemoteSessionEmaBinding, previous int64) (repo.RemoteSessionEmaBinding, error) {
	result, err := q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, RemoteSessionClientID: b.RemoteSessionClientID, Generation: b.Generation, ExpectedGeneration: previous, State: conv.ToPGText(preparationBindingState(b.State)), GrantSource: conv.ToPGText(preparationBindingGrantSource(b.GrantSource)), RequestedScopes: b.RequestedScopes, ClaimID: b.ClaimID, ClaimedAt: b.ClaimedAt})
	if err != nil {
		return result, oops.E(oops.CodeUnexpected, err, "persist preparation binding")
	}
	return result, nil
}

// PrepareIdentityChaining is the only registration entrypoint. Discovery never
// invokes it. Its durable DCR claim commits before HTTP and cannot be replayed.
func (s *Service) PrepareIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.prepareIdentityChaining(ctx, in, false, false)
}
func (s *Service) UnlinkIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.prepareIdentityChaining(ctx, in, true, false)
}
func (s *Service) ReadIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.prepareIdentityChaining(ctx, in, false, true)
}

func (s *Service) prepareIdentityChaining(ctx context.Context, in PreparationInput, unlink, read bool) (*PreparationResult, error) {
	var emptyClient repo.RemoteSessionClient
	project, org, err := s.preparationTenant(ctx, !read)
	if err != nil {
		return nil, err
	}
	in, err = normalizePreparationInput(in)
	if err != nil {
		return nil, err
	}
	// A connection-scoped lock serializes this binding across the claim commit,
	// HTTP submission and outcome commit without an open transaction during HTTP.
	// Process/connection loss releases the lock; the durable claim still prevents
	// replay of an uncertain registration.
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock preparation binding")
	}
	defer conn.Release()
	lockKey := strings.Join([]string{"ema-preparation", org, project.String(), in.UserSessionIssuerID.String(), in.RemoteSessionIssuerID.String(), in.Resource}, "\n")
	lockRepo := repo.New(conn)
	if err := lockRepo.LockPreparationSubmission(ctx, lockKey); err != nil {
		// A canceled lock request can have acquired the lock at the server.
		_ = conn.Conn().Close(context.Background())
		return nil, oops.E(oops.CodeUnexpected, err, "lock preparation binding")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lockRepo.UnlockPreparationSubmission(unlockCtx, lockKey); err != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}()
	// The shared registration lock closes the preflight-to-HTTP race with
	// interactive rotation. Reject foreign IDs before taking its global key.
	if _, err := lockRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org), IncludeOrganizational: true, IncludeGlobal: true}); err != nil {
		return nil, preparationLookupError(err, "remote issuer not found")
	}
	releaseRegistration, err := lockRegistrationIssuer(ctx, conn, in.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuer registration")
	}
	defer releaseRegistration()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := repo.New(tx)
	// Lock the project before parents, including wholly inherited configurations.
	if _, err = q.LockEMAProject(ctx, repo.LockEMAProjectParams{ProjectID: project, OrganizationID: org}); err != nil {
		return nil, preparationLookupError(err, "project not found")
	}
	if err = lockUserSessionIssuersForClientBinding(ctx, s.logger, tx, q, project, org, []uuid.UUID{in.UserSessionIssuerID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	if _, err = q.LockEMAUserIssuer(ctx, repo.LockEMAUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)}); err != nil {
		return nil, preparationLookupError(err, "user session issuer not found")
	}
	issuer, err := q.LockEMAIssuer(ctx, repo.LockEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
	if err != nil {
		return nil, preparationLookupError(err, "remote issuer not found")
	}
	if m := in.ResourceMetadata; m != nil && (m.Resource != in.Resource || !slices.Contains(m.AuthorizationServers, issuer.Issuer)) {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource authorization server association mismatch")
	}
	// Read the client pointer before the binding lock; issuer lock serializes all
	// preparation changes for this issuer and establishes lifecycle lock ordering.
	key := repo.GetEMABindingParams{ProjectID: project, OrganizationID: org, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	b, err := q.GetEMABinding(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		if read || unlink {
			state := "configuration_required"
			if read {
				if eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported); eligibility != "eligible" {
					state = eligibility
				} else if preparationMetadataTransient(issuer) {
					state = "transient_failure"
				}
			}
			r := preparationDiagnostic(state)
			r.Resource = in.Resource
			r.Issuer = issuer.Issuer
			return r, nil
		}
		err = q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		b, err = q.GetEMABinding(ctx, key)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	selected := in.ClientID
	if selected == uuid.Nil && b.RemoteSessionClientID.Valid {
		selected = b.RemoteSessionClientID.UUID
	}
	var client repo.RemoteSessionClient
	if selected != uuid.Nil {
		client, err = q.LockEMAClient(ctx, repo.LockEMAClientParams{ID: selected, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
		if err != nil {
			return nil, preparationLookupError(err, "selected client not found")
		}
		if client.RemoteSessionIssuerID != issuer.ID {
			return nil, oops.E(oops.CodeBadRequest, nil, "selected client belongs to another issuer")
		}
	}
	b, err = q.LockEMABinding(ctx, repo.LockEMABindingParams(key))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	previous := b.Generation
	if unlink {
		if in.ExpectedGeneration != b.Generation {
			return preparationResult(b, issuer, client, "configuration_required"), oops.E(oops.CodeConflict, nil, "binding generation changed; read current preparation before unlinking")
		}
		b.Generation++
		b.State = conv.ToPGText("unlinked")
		b.RemoteSessionClientID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		b.ClaimID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		b.ClaimedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		b.GrantSource = conv.ToPGText("unknown")
		b.RequestedScopes = []string{}
		b, err = setPreparationBinding(ctx, q, b, previous)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		return preparationResult(b, issuer, emptyClient, preparationBindingState(b.State)), nil
	}
	if b.ClaimID.Valid && in.ClientID == uuid.Nil && (read || preparationBindingState(b.State) != "provider_rejection" || in.ExpectedGeneration != b.Generation) {
		state := preparationBindingState(b.State)
		if state == "in_progress" && (!b.ClaimedAt.Valid || time.Since(b.ClaimedAt.Time) > time.Minute) {
			state = "indeterminate"
			b.State = conv.ToPGText(state)
			b, err = setPreparationBinding(ctx, q, b, previous)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
			}
			if err = tx.Commit(ctx); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
			}
		}
		return preparationResult(b, issuer, client, state), nil
	}
	// Registration evidence is durable; readiness is not. Recompute completed
	// provider registrations without replaying DCR or replacing effective scopes.
	if (read || (in.Mechanism == "dcr" && in.ClientID == uuid.Nil)) && b.RemoteSessionClientID.Valid && preparationBindingGrantSource(b.GrantSource) == "provider_returned" {
		return preparationResult(b, issuer, client, preparationRegistrationReadiness(ctx, q, client, issuer, org)), nil
	}
	eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported)
	if eligibility != "eligible" {
		return preparationResult(b, issuer, client, eligibility), nil
	}
	if preparationMetadataTransient(issuer) {
		return preparationResult(b, issuer, client, "transient_failure"), nil
	}
	if (read || in.Mechanism == "dcr") && (preparationBindingState(b.State) == "ready" || preparationBindingState(b.State) == "published_acceptance_unverified") {
		if !preparationClientConfigurationValid(ctx, q, client, issuer, org) {
			return preparationResult(b, issuer, client, "manual_setup_required"), nil
		}
		if !slices.Contains(client.GrantTypes, PreparationJWTBearerGrant) {
			return preparationResult(b, issuer, client, preparationMissingGrantsState(client.GrantTypes)), nil
		}
	}
	if read {
		return preparationResult(b, issuer, client, preparationBindingState(b.State)), nil
	}
	changed := preparationBindingState(b.State) == "provider_rejection" || selected != b.RemoteSessionClientID.UUID || preparationBindingState(b.State) == "unlinked" || !slices.Equal(b.RequestedScopes, in.Scopes) || (in.ConfirmGrants != nil && (!samePreparationGrants(in.ConfirmGrants, client.GrantTypes) || preparationBindingGrantSource(b.GrantSource) != "administrator_declared")) || (in.Mechanism == "cimd" && (!slices.Contains(client.GrantTypes, PreparationJWTBearerGrant) || preparationBindingGrantSource(b.GrantSource) != "cimd_published"))
	if changed && (b.RemoteSessionClientID.Valid || b.ClaimID.Valid || preparationBindingState(b.State) == "unlinked") && in.ExpectedGeneration != b.Generation {
		return preparationResult(b, issuer, client, "configuration_required"), nil
	}
	if selected == uuid.Nil {
		if in.Mechanism != "dcr" {
			return preparationResult(b, issuer, client, "configuration_required"), nil
		}
		method := in.TokenEndpointAuthMethod
		if !issuer.RegistrationEndpoint.Valid || (method != "client_secret_basic" && method != "client_secret_post") || !slices.Contains(issuer.TokenEndpointAuthMethodsSupported, method) {
			return preparationResult(b, issuer, client, "manual_setup_required"), nil
		}
		if !urls.IsAbsoluteHTTPSOrLoopback(issuer.RegistrationEndpoint.String) {
			return preparationResult(b, issuer, client, "manual_setup_required"), nil
		}
		// Only mutate the returned binding once the request can be persisted.
		if changed {
			b.Generation++
		}
		b.RequestedScopes = in.Scopes
		b.State = conv.ToPGText("in_progress")
		b.ClaimID = conv.ToNullUUID(uuid.New())
		b.ClaimedAt = conv.ToPGTimestamptz(time.Now())
		b.GrantSource = conv.ToPGText("unknown")
		b, err = setPreparationBinding(ctx, q, b, previous)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		return s.finishPreparationDCR(ctx, conn, in, b, issuer, method)
	}
	// Explicit selection is never inferred from interactive attachments or grants.
	if in.Mechanism == "dcr" {
		return preparationResult(b, issuer, client, "configuration_required"), nil
	}
	grants := client.GrantTypes
	source := preparationBindingGrantSource(b.GrantSource)
	if selected != b.RemoteSessionClientID.UUID {
		source = "administrator_declared"
	}
	if in.ConfirmGrants != nil {
		grants = slices.Clone(in.ConfirmGrants)
		source = "administrator_declared"
	}
	state := "ready"
	if source == "cimd_published" {
		state = "published_acceptance_unverified"
	}
	if in.Mechanism == "cimd" {
		if !client.ClientIDMetadataUri.Valid || !issuer.ClientIDMetadataDocumentSupported {
			return preparationResult(b, issuer, client, "manual_setup_required"), nil
		}
		if client.GrantTypes == nil {
			grants = append(grants, "authorization_code", "refresh_token")
		} else {
			grants = append(slices.Clone(grants), client.GrantTypes...)
		}
		slices.Sort(grants)
		grants = slices.Compact(grants)
		if !slices.Contains(grants, PreparationJWTBearerGrant) {
			grants = append(slices.Clone(grants), PreparationJWTBearerGrant)
		}
		source = "cimd_published"
		state = "published_acceptance_unverified"
	}
	if !slices.Contains(grants, PreparationJWTBearerGrant) {
		state = "unknown_grants"
		if grants != nil {
			state = "manual_setup_required"
		}
	}
	if (state == "ready" || state == "published_acceptance_unverified") && !preparationClientConfigurationValid(ctx, q, client, issuer, org) {
		return preparationResult(b, issuer, client, "manual_setup_required"), nil
	}
	if client.Scope != nil {
		for _, scope := range in.Scopes {
			if !slices.Contains(client.Scope, scope) {
				return preparationResult(b, issuer, client, "manual_setup_required"), nil
			}
		}
	}
	if !samePreparationGrants(grants, client.GrantTypes) {
		// A grant publication is client-wide. Do not silently reconfigure another
		// resource binding without invalidating its generation as well.
		count, countErr := q.CountActiveEMABindingsForClient(ctx, repo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(client.ID), OrganizationID: org, ProjectID: uuid.Nil})
		if countErr != nil {
			return nil, oops.E(oops.CodeUnexpected, countErr, "count client bindings")
		}
		allowed := int64(0)
		if b.RemoteSessionClientID.Valid && b.RemoteSessionClientID.UUID == client.ID && preparationBindingState(b.State) != "unlinked" {
			allowed = 1
		}
		if count > allowed {
			return preparationResult(b, issuer, client, "configuration_required"), nil
		}
		// Publishing/modifying inherited registrations affects other projects and
		// therefore requires organization-level authority in addition to project write.
		if !client.ProjectID.Valid {
			if err = s.authz.Require(ctx, authz.Check{ResourceKind: "", Dimensions: nil, Scope: authz.ScopeOrgAdmin, ResourceID: org}); err != nil {
				return nil, err
			}
		}
		client, err = q.SetEMAClientGrants(ctx, repo.SetEMAClientGrantsParams{ID: client.ID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org), GrantTypes: grants})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
	}
	if grants == nil {
		source = "unknown"
	}
	// All configuration-required exits above return the persisted generation
	// and scopes, not a prospective transition that will be rolled back.
	if changed {
		b.Generation++
	}
	b.RequestedScopes = in.Scopes
	b.RemoteSessionClientID = conv.ToNullUUID(client.ID)
	b.State = conv.ToPGText(state)
	b.GrantSource = conv.ToPGText(source)
	b.ClaimID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	b.ClaimedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	b, err = setPreparationBinding(ctx, q, b, previous)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	return preparationResult(b, issuer, client, state), nil
}

// A completed provider registration can recover as discovery or credentials
// change, but only current grant evidence can make it ready.
func preparationRegistrationReadiness(ctx context.Context, q *repo.Queries, client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer, org string) string {
	if eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported); eligibility != "eligible" {
		return eligibility
	}
	if preparationMetadataTransient(issuer) {
		return "transient_failure"
	}
	if !preparationClientConfigurationValid(ctx, q, client, issuer, org) {
		return "manual_setup_required"
	}
	if !slices.Contains(client.GrantTypes, PreparationJWTBearerGrant) {
		return preparationMissingGrantsState(client.GrantTypes)
	}
	return "ready"
}

// Readiness is a current local configuration check, never a provider acceptance
// claim. Reads and idempotent DCR lookups must not keep expired credentials ready.
func preparationClientConfigurationValid(ctx context.Context, q *repo.Queries, client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer, org string) bool {
	method := client.TokenEndpointAuthMethod.String
	if client.ClientSecretExpiresAt.Valid && !client.ClientSecretExpiresAt.Time.After(time.Now()) {
		return false
	}
	authMethodSupported := slices.Contains(issuer.TokenEndpointAuthMethodsSupported, method) || (method == "none" && len(issuer.TokenEndpointAuthMethodsSupported) == 0)
	if method == "" || !authMethodSupported || ((method == "client_secret_basic" || method == "client_secret_post") && !client.ClientSecretEncrypted.Valid) || (method == "private_key_jwt" && !client.JsonWebKeySetID.Valid) {
		return false
	}
	if method == "private_key_jwt" && client.JsonWebKeySetID.Valid {
		if _, err := q.LockJsonWebKeySetForClientAttach(ctx, repo.LockJsonWebKeySetForClientAttachParams{ID: client.JsonWebKeySetID.UUID, OrganizationID: org}); err != nil {
			return false
		}
	}
	return true
}

func preparationLookupError(err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "%s", message)
	}
	return oops.E(oops.CodeUnexpected, err, "failed to lock preparation resource")
}

func preparationMissingGrantsState(grants []string) string {
	if grants == nil {
		return "unknown_grants"
	}
	return "manual_setup_required"
}

func preparationMetadataTransient(issuer repo.RemoteSessionIssuer) bool {
	_, transient := issuerMetadataFailureState(IssuerMetadataUse{
		ID: issuer.ID, IssuerURL: issuer.Issuer, ProjectID: issuer.ProjectID, OrganizationID: issuer.OrganizationID,
		MetadataFetchedAt: issuer.MetadataFetchedAt, MetadataLastErrorAt: issuer.MetadataLastErrorAt, MetadataLastErrorUrl: issuer.MetadataLastErrorUrl, NeedsReprojection: false,
	})
	return transient
}
