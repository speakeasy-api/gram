package networkingress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/ingresscredentials"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// ExecutorOptions contains only runtime configuration, never provider credentials.
type ExecutorOptions struct {
	// Queue is the authoritative reconciliation task queue.
	Queue string

	// CanApply is checked on every attempt before credential decryption.
	CanApply func(context.Context) error

	// Logger receives operational failures that are not reflected in the
	// bounded status codes, such as saved credentials that no longer decrypt.
	// Nil discards them.
	Logger *slog.Logger

	// PublicationRequester atomically records organization invalidations for
	// committed DNS authority changes. A nil requester keeps the producer off.
	PublicationRequester PublicationRequester

	// PublicationActor identifies the reconciler actor in publication hints.
	PublicationActor string
}

// ReconcileResult is safe to persist in orchestration history.
type ReconcileResult struct {
	// Requeue means desired state changed during this pass.
	Requeue bool
}

// ReconcileCodeDeletionPending reports a teardown waiting for provider
// resources to finish deleting. It is retryable and usually clears within
// seconds.
const ReconcileCodeDeletionPending = "deletion_pending"

// ReconcileError deliberately has no wrapped cause or provider-supplied message.
type ReconcileError struct {
	// Code is a bounded diagnostic suitable for orchestration history.
	Code string

	// Retryable controls retries within the current workflow run.
	Retryable bool
}

func (e *ReconcileError) Error() string { return "network ingress reconciliation: " + e.Code }

// Executor reconciles one ingress at a time across all workers and attempts.
type Executor struct {
	db       *pgxpool.Pool
	creds    *ingresscredentials.Credentials
	registry *k8s.NetworkIngressProvisionerRegistry
	options  ExecutorOptions
}

func NewExecutor(db *pgxpool.Pool, creds *ingresscredentials.Credentials, registry *k8s.NetworkIngressProvisionerRegistry, options ExecutorOptions) *Executor {
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Executor{db: db, creds: creds, registry: registry, options: options}
}

func (e *Executor) Reconcile(ctx context.Context, organizationID string, id uuid.UUID) (ReconcileResult, error) {
	if organizationID == "" || id == uuid.Nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("invalid_desired_state")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	conn, err := e.db.Acquire(ctx)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	queries := repo.New(conn)
	// Keep the legacy UUID lock key while workflow IDs migrate away from their
	// queue-qualified form. Old and new workflow executions then remain mutually
	// exclusive, while every row read and write is independently organization-scoped.
	lockKey := id.String()
	locked, err := queries.TryAcquireNetworkIngressReconcileLock(ctx, lockKey)
	if err != nil {
		// An interrupted lock query may have acquired its session lock. Never
		// return an ambiguously locked connection to the pool.
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer closeCancel()
		o11y.NoLogDefer(func() error { return conn.Hijack().Close(closeCtx) })
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if !locked {
		conn.Release()
		return ReconcileResult{Requeue: false}, reconcileFailure("reconciliation_busy")
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer unlockCancel()
		released, err := queries.ReleaseNetworkIngressReconcileLock(unlockCtx, lockKey)
		if err != nil || !released {
			o11y.NoLogDefer(func() error { return conn.Hijack().Close(unlockCtx) })
			return
		}
		conn.Release()
	}()

	row, err := queries.GetNetworkIngressForReconcile(ctx, repo.GetNetworkIngressForReconcileParams{ID: id, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconcileResult{Requeue: false}, nil
	}
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if row.Deleted {
		return e.cleanup(ctx, conn, row)
	}
	resources, err := k8s.ParseNetworkIngressResourceNames(row.ProviderResources)
	if err != nil || resources.OwnerID != row.ID {
		return e.record(ctx, conn, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure("invalid_desired_state"), "")
	}
	// Only the tailnet gateway hosts ingresses. A row created for the retired
	// Kubernetes operator is never applied, since gateway pods only host
	// gateway rows; deleting it still cleans up below.
	if row.Runtime.String != RuntimeGateway {
		return e.record(ctx, conn, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure("unsupported_provider"), "")
	}
	provider, err := e.registry.Provisioner(row.Provider)
	if err != nil {
		code := "unsupported_provider"
		if row.Provider == ProviderTailscale {
			code = "provider_configuration_unavailable"
		}
		return e.record(ctx, conn, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure(code), "")
	}

	gateCode := ""
	if row.Enabled {
		gateCode = e.applyGate(ctx)
	}
	var observation k8s.NetworkIngressObservation
	var operationErr error
	if row.Enabled && gateCode == "" {
		// Reload after the gate: API writes do not wait for this session lock.
		current, err := queries.GetNetworkIngressForReconcile(ctx, repo.GetNetworkIngressForReconcileParams{ID: id, OrganizationID: organizationID})
		if err != nil {
			return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
		}
		if current.Deleted {
			return e.cleanup(ctx, conn, current)
		}
		if !sameIngressDesired(row, current) {
			return ReconcileResult{Requeue: true}, nil
		}
		gateCode = e.applyGate(ctx)
		if gateCode == "" {
			credentials, err := e.creds.Open(ctx, conn, row.OrganizationID, ingresscredentials.Stored(row))
			if err != nil || len(credentials) == 0 {
				return e.record(ctx, conn, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, e.credentialFailure(ctx, row, err), "")
			}
			callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
			observation, operationErr = provider.Apply(callCtx, k8s.NetworkIngressDesired{
				ID: row.ID, OrganizationID: row.OrganizationID, Provider: row.Provider, Hostname: row.Hostname, Credentials: credentials, Resources: resources,
			})
			callCancel()
		}
	}
	if !row.Enabled || gateCode != "" {
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
		observation, operationErr = provider.Observe(callCtx, resources)
		callCancel()
		// A retryable Observe failure that reports no name says nothing about
		// the hosts serving the recorded one, so the private listener keeps
		// routing to it until an observation succeeds.
		if operationErr != nil && observation.DNSName == "" && row.DnsName.Valid && providerFailure(operationErr, observation.ErrorCode).Retryable {
			observation.DNSName = row.DnsName.String
		}
	}

	// Even a failed or partially successful Apply can have created resources.
	// Deletion wins before any observation or provider error is returned.
	current, err := queries.GetNetworkIngressForReconcile(ctx, repo.GetNetworkIngressForReconcileParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if current.Deleted {
		return e.cleanup(ctx, conn, current)
	}
	if !sameIngressDesired(row, current) {
		return ReconcileResult{Requeue: true}, nil
	}
	var failure *ReconcileError
	if operationErr != nil {
		failure = providerFailure(operationErr, observation.ErrorCode)
	}
	return e.record(ctx, conn, row, observation, failure, gateCode)
}

func (e *Executor) applyGate(ctx context.Context) string {
	if e.creds == nil || e.options.Queue == "" {
		return "provider_configuration_unavailable"
	}
	if e.options.CanApply == nil || e.options.CanApply(ctx) != nil {
		return "provider_mutations_disabled"
	}
	return ""
}

func (e *Executor) cleanup(ctx context.Context, conn *pgxpool.Conn, row repo.NetworkIngress) (ReconcileResult, error) {
	if !ingresscredentials.Has(ingresscredentials.Stored(row)) && bytes.Equal(row.ProviderResources, []byte("{}")) {
		return ReconcileResult{Requeue: false}, nil
	}
	resources, err := k8s.ParseNetworkIngressResourceNames(row.ProviderResources)
	if err != nil || resources.OwnerID != row.ID {
		return ReconcileResult{Requeue: false}, reconcileFailure("invalid_desired_state")
	}
	provider, err := e.registry.Provisioner(row.Provider)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("provider_configuration_unavailable")
	}
	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
	err = e.deleteProviderResources(callCtx, provider, row, resources)
	callCancel()
	if err != nil {
		failure := providerFailure(err, "")
		// A missing permission blocks cleanup the same way rejected
		// credentials do, and the dashboard explains both with one message.
		blocker := ""
		if errors.Is(err, k8s.ErrNetworkIngressProviderCredentialsRejected) {
			blocker = k8s.NetworkIngressErrorProviderCredentialsRejected
		}
		if err := recordCleanupBlocker(ctx, repo.New(conn), row, blocker); err != nil {
			return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
		}
		return ReconcileResult{Requeue: false}, failure
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	defer o11y.NoLogDefer(func() error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return tx.Rollback(rollbackCtx)
	})
	queries := repo.New(tx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, row.OrganizationID); err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	current, err := queries.LockNetworkIngressForReconcile(ctx, repo.LockNetworkIngressForReconcileParams{ID: row.ID, OrganizationID: row.OrganizationID})
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if !current.Deleted || !sameIngressDesired(row, current) {
		return ReconcileResult{Requeue: true}, nil
	}
	if _, err := queries.ClearDeletedNetworkIngressResources(ctx, repo.ClearDeletedNetworkIngressResourcesParams{ID: row.ID, OrganizationID: row.OrganizationID}); err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	return ReconcileResult{Requeue: false}, nil
}

// deleteProviderResources hands the saved credentials to providers whose
// teardown calls the customer's provider API. Such a provider always receives
// the credentialed call, with nil credentials when none are saved or they no
// longer decrypt, so it decides whether cleanup can finish without them.
func (e *Executor) deleteProviderResources(ctx context.Context, provider k8s.NetworkIngressProvisioner, row repo.NetworkIngress, resources k8s.NetworkIngressResourceNames) error {
	deleter, ok := provider.(k8s.NetworkIngressCredentialedDeleter)
	if !ok {
		return provider.Delete(ctx, resources) //nolint:wrapcheck // provider errors are classified by providerFailure
	}
	var credentials []byte
	if e.creds != nil && ingresscredentials.Has(ingresscredentials.Stored(row)) {
		plaintext, err := e.creds.Open(ctx, nil, row.OrganizationID, ingresscredentials.Stored(row))
		switch {
		case err != nil && !orgencryption.IsUnreadable(err):
			// The key may come back, and teardown without the credentials
			// could strand resources on the customer's tailnet, so retry.
			e.options.Logger.ErrorContext(ctx, "saved network ingress credentials cannot be opened yet; cleanup waits for the organization key",
				attr.SlogError(err), attr.SlogNetworkIngressID(row.ID.String()))
			return reconcileFailure(k8s.NetworkIngressErrorEncryptionKeyUnavailable)
		case err != nil:
			// Distinguishable from "nothing saved": the provider still reports
			// the cleanup blocker, and this names the server-side cause.
			e.options.Logger.ErrorContext(ctx, "saved network ingress credentials do not decrypt; cleanup proceeds without them",
				attr.SlogError(err), attr.SlogNetworkIngressID(row.ID.String()))
		case len(plaintext) > 0:
			credentials = plaintext
		}
	}
	return deleter.DeleteWithCredentials(ctx, resources, credentials) //nolint:wrapcheck // provider errors are classified by providerFailure
}

// recordCleanupBlocker exposes why a tombstone cannot finish cleanup, or clears
// a previous blocker once teardown is progressing again.
func recordCleanupBlocker(ctx context.Context, queries *repo.Queries, row repo.NetworkIngress, code string) error {
	current := ""
	if row.LastError.Valid {
		current = row.LastError.String
	}
	if current == code {
		return nil
	}
	_, err := queries.RecordDeletedNetworkIngressCleanupError(ctx, repo.RecordDeletedNetworkIngressCleanupErrorParams{
		ID: row.ID, OrganizationID: row.OrganizationID, ExpectedUpdatedAt: row.UpdatedAt, LastError: conv.ToPGTextEmpty(code),
	})
	if err != nil {
		return fmt.Errorf("record network ingress cleanup blocker: %w", err)
	}
	return nil
}

func (e *Executor) record(ctx context.Context, conn *pgxpool.Conn, row repo.NetworkIngress, observation k8s.NetworkIngressObservation, failure *ReconcileError, gateCode string) (ReconcileResult, error) {
	status := observation.Status
	code := observation.ErrorCode
	switch code {
	case "", "invalid_desired_state", "unsupported_provider", "invalid_credentials",
		k8s.NetworkIngressErrorProviderForbidden, k8s.NetworkIngressErrorProviderRateLimited, k8s.NetworkIngressErrorProviderAPIUnavailable,
		k8s.NetworkIngressErrorProviderServiceNotApproved, k8s.NetworkIngressErrorProviderCertificateUnavailable,
		k8s.NetworkIngressErrorEncryptionKeyUnavailable:
	default:
		code = "provider_error"
		failure = reconcileFailure(code)
	}
	switch status {
	case "pending", "online", "degraded", "error":
	default:
		status = "error"
		if failure == nil {
			failure = reconcileFailure("provider_error")
		}
	}
	if failure != nil {
		status, code = "error", failure.Code
	} else if code != "" {
		status = "error"
	}
	if gateCode != "" && failure == nil {
		code = gateCode
		// Observe can describe the previous resources, but cannot confirm a
		// pending credential/hostname/identity change was applied.
		if row.Status == "pending" || !row.HealthCheckedAt.Valid || row.HealthCheckedAt.Time.Before(row.UpdatedAt.Time) {
			status = "pending"
		}
	}
	dns := observation.DNSName
	if dns != "" && len(k8svalidation.IsDNS1123Subdomain(dns)) != 0 {
		dns = ""
		status, code = "error", "provider_error"
		failure = reconcileFailure(code)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	defer o11y.NoLogDefer(func() error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return tx.Rollback(rollbackCtx)
	})
	queries := repo.New(tx)
	current, err := queries.LockNetworkIngressForReconcile(ctx, repo.LockNetworkIngressForReconcileParams{ID: row.ID, OrganizationID: row.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconcileResult{Requeue: false}, nil
	}
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if !sameIngressDesired(row, current) {
		return ReconcileResult{Requeue: true}, nil
	}
	previousDNS := ""
	if current.DnsName.Valid {
		previousDNS = current.DnsName.String
	}
	count, err := queries.RecordNetworkIngressObservation(ctx, repo.RecordNetworkIngressObservationParams{
		ID: current.ID, OrganizationID: current.OrganizationID, ExpectedUpdatedAt: current.UpdatedAt, Status: status, DnsName: conv.ToPGTextEmpty(dns), LastError: conv.ToPGTextEmpty(code),
	})
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if count == 0 {
		return ReconcileResult{Requeue: true}, nil
	}
	if previousDNS != dns && e.options.PublicationRequester != nil {
		if err := e.options.PublicationRequester.Organization(ctx, tx, current.OrganizationID, e.options.PublicationActor); err != nil {
			return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if failure != nil {
		return ReconcileResult{Requeue: false}, failure
	}
	return ReconcileResult{Requeue: false}, nil
}

func sameIngressDesired(a, b repo.NetworkIngress) bool {
	return a.UpdatedAt == b.UpdatedAt && a.Deleted == b.Deleted && a.Enabled == b.Enabled && a.IdentityRequired == b.IdentityRequired &&
		a.Hostname == b.Hostname && a.Provider == b.Provider && a.CredentialsEncrypted == b.CredentialsEncrypted && a.CredentialsOrgEncrypted == b.CredentialsOrgEncrypted &&
		a.OrganizationID == b.OrganizationID && a.EndpointNamespaceKind == b.EndpointNamespaceKind && a.CustomDomainID == b.CustomDomainID &&
		a.Runtime == b.Runtime && bytes.Equal(a.ProviderResources, b.ProviderResources)
}

func providerFailure(err error, code string) *ReconcileError {
	switch {
	case errors.Is(err, k8s.ErrNetworkIngressProviderForbidden):
		return reconcileFailure(k8s.NetworkIngressErrorProviderForbidden)
	case errors.Is(err, k8s.ErrNetworkIngressProviderCredentialsRejected):
		return reconcileFailure(k8s.NetworkIngressErrorProviderCredentialsRejected)
	case errors.Is(err, k8s.ErrNetworkIngressProviderRateLimited):
		return reconcileFailure(k8s.NetworkIngressErrorProviderRateLimited)
	case errors.Is(err, k8s.ErrNetworkIngressProviderAPIUnavailable):
		return reconcileFailure(k8s.NetworkIngressErrorProviderAPIUnavailable)
	case errors.Is(err, k8s.ErrNetworkIngressDeletionPending):
		return reconcileFailure(ReconcileCodeDeletionPending)
	case errors.Is(err, k8s.ErrNetworkIngressInvalidDesiredState):
		return reconcileFailure("invalid_desired_state")
	case errors.Is(err, k8s.ErrNetworkIngressUnsupportedProvider):
		return reconcileFailure("unsupported_provider")
	}
	switch code {
	case "invalid_desired_state", "unsupported_provider", "invalid_credentials",
		k8s.NetworkIngressErrorProviderForbidden, k8s.NetworkIngressErrorProviderRateLimited, k8s.NetworkIngressErrorProviderAPIUnavailable,
		k8s.NetworkIngressErrorEncryptionKeyUnavailable:
		return reconcileFailure(code)
	default:
		return reconcileFailure("provider_error")
	}
}

// credentialFailure classifies why saved credentials could not be opened for
// Apply. Missing or undecryptable credentials need new ones from a person;
// any other failure is the organization key, which may come back, so the
// reconcile retries.
func (e *Executor) credentialFailure(ctx context.Context, row repo.NetworkIngress, err error) *ReconcileError {
	if err == nil || orgencryption.IsUnreadable(err) {
		return reconcileFailure("invalid_credentials")
	}
	e.options.Logger.ErrorContext(ctx, "open saved network ingress credentials",
		attr.SlogError(err), attr.SlogNetworkIngressID(row.ID.String()), attr.SlogOrganizationID(row.OrganizationID))
	return reconcileFailure(k8s.NetworkIngressErrorEncryptionKeyUnavailable)
}

func reconcileFailure(code string) *ReconcileError {
	retryable := true
	switch code {
	// Rejected provider credentials need a human to fix them. The hourly sweep
	// and "Retry cleanup" re-drive the ingress instead of spinning here.
	case "invalid_desired_state", "unsupported_provider", "invalid_credentials", "provider_configuration_unavailable",
		k8s.NetworkIngressErrorProviderCredentialsRejected, k8s.NetworkIngressErrorProviderForbidden:
		retryable = false
	}
	return &ReconcileError{Code: code, Retryable: retryable}
}
