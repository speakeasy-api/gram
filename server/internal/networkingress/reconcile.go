package networkingress

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ExecutorOptions contains only runtime configuration, never provider credentials.
type ExecutorOptions struct {
	// Queue is the authoritative reconciliation task queue.
	Queue string

	// Image is the attestor image used by Apply.
	Image string

	// BackendService is the private backend Service name.
	BackendService string

	// BackendPort is the private backend Service port.
	BackendPort int32

	// CanApply is checked on every attempt before credential decryption.
	CanApply func(context.Context) error
}

// ReconcileResult is safe to persist in orchestration history.
type ReconcileResult struct {
	// Requeue means desired state changed during this pass.
	Requeue bool
}

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
	enc      *encryption.Client
	registry *k8s.NetworkIngressProvisionerRegistry
	options  ExecutorOptions
}

func NewExecutor(db *pgxpool.Pool, enc *encryption.Client, registry *k8s.NetworkIngressProvisionerRegistry, options ExecutorOptions) *Executor {
	return &Executor{db: db, enc: enc, registry: registry, options: options}
}

func (e *Executor) Reconcile(ctx context.Context, id uuid.UUID) (ReconcileResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	conn, err := e.db.Acquire(ctx)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	queries := repo.New(conn)
	locked, err := queries.TryAcquireNetworkIngressReconcileLock(ctx, id.String())
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
		released, err := queries.ReleaseNetworkIngressReconcileLock(unlockCtx, id.String())
		if err != nil || !released {
			o11y.NoLogDefer(func() error { return conn.Hijack().Close(unlockCtx) })
			return
		}
		conn.Release()
	}()

	row, err := queries.GetNetworkIngressForReconcile(ctx, id)
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
	if err != nil || resources.OwnerID != row.ID || resources.Namespace != row.AttestorNamespace || resources.AttestorServiceAccount != row.AttestorServiceAccount {
		return e.record(ctx, queries, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure("invalid_desired_state"), "")
	}
	provider, err := e.registry.Provisioner(row.Provider)
	if err != nil {
		code := "unsupported_provider"
		if row.Provider == ProviderTailscale {
			code = "provider_configuration_unavailable"
		}
		return e.record(ctx, queries, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure(code), "")
	}

	gateCode := ""
	if row.Enabled {
		gateCode = e.applyGate(ctx)
	}
	var observation k8s.NetworkIngressObservation
	var operationErr error
	if row.Enabled && gateCode == "" {
		// Reload after the gate: API writes do not wait for this session lock.
		current, err := queries.GetNetworkIngressForReconcile(ctx, id)
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
			credentials, err := e.enc.Decrypt(row.CredentialsEncrypted.String)
			if err != nil || !row.CredentialsEncrypted.Valid || credentials == "" {
				return e.record(ctx, queries, row, k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, reconcileFailure("invalid_credentials"), "")
			}
			callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
			observation, operationErr = provider.Apply(callCtx, k8s.NetworkIngressDesired{
				ID: row.ID, Provider: row.Provider, Hostname: row.Hostname, Credentials: []byte(credentials), Resources: resources,
				AttestorImage: e.options.Image, BackendService: e.options.BackendService, BackendPort: e.options.BackendPort,
			})
			callCancel()
		}
	}
	if !row.Enabled || gateCode != "" {
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
		observation, operationErr = provider.Observe(callCtx, resources)
		callCancel()
	}

	// Even a failed or partially successful Apply can have created resources.
	// Deletion wins before any observation or provider error is returned.
	current, err := queries.GetNetworkIngressForReconcile(ctx, id)
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
	return e.record(ctx, queries, row, observation, failure, gateCode)
}

func (e *Executor) applyGate(ctx context.Context) string {
	if e.enc == nil || e.options.Queue == "" || e.options.Image == "" || e.options.BackendService == "" || e.options.BackendPort <= 0 || e.options.BackendPort > 65535 {
		return "provider_configuration_unavailable"
	}
	if e.options.CanApply == nil || e.options.CanApply(ctx) != nil {
		return "provider_mutations_disabled"
	}
	return ""
}

func (e *Executor) cleanup(ctx context.Context, conn *pgxpool.Conn, row repo.NetworkIngress) (ReconcileResult, error) {
	if !row.CredentialsEncrypted.Valid && bytes.Equal(row.ProviderResources, []byte("{}")) {
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
	err = provider.Delete(callCtx, resources)
	callCancel()
	if err != nil {
		return ReconcileResult{Requeue: false}, providerFailure(err, "")
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	queries := repo.New(tx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, row.OrganizationID); err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	current, err := queries.LockNetworkIngressForReconcile(ctx, row.ID)
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

func (e *Executor) record(ctx context.Context, queries *repo.Queries, row repo.NetworkIngress, observation k8s.NetworkIngressObservation, failure *ReconcileError, gateCode string) (ReconcileResult, error) {
	status := observation.Status
	code := observation.ErrorCode
	switch code {
	case "", "invalid_desired_state", "unsupported_provider", "invalid_credentials", "kubernetes_api":
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
	if gateCode != "" {
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
	count, err := queries.RecordNetworkIngressObservation(ctx, repo.RecordNetworkIngressObservationParams{
		ID: row.ID, ExpectedUpdatedAt: row.UpdatedAt, Status: status, DnsName: conv.ToPGTextEmpty(dns), LastError: conv.ToPGTextEmpty(code),
	})
	if err != nil {
		return ReconcileResult{Requeue: false}, reconcileFailure("database_unavailable")
	}
	if count == 0 {
		return ReconcileResult{Requeue: true}, nil
	}
	if failure != nil {
		return ReconcileResult{Requeue: false}, failure
	}
	return ReconcileResult{Requeue: false}, nil
}

func sameIngressDesired(a, b repo.NetworkIngress) bool {
	return a.UpdatedAt == b.UpdatedAt && a.Deleted == b.Deleted && a.Enabled == b.Enabled && a.IdentityRequired == b.IdentityRequired &&
		a.Hostname == b.Hostname && a.Provider == b.Provider && a.CredentialsEncrypted == b.CredentialsEncrypted &&
		a.OrganizationID == b.OrganizationID && a.EndpointNamespaceKind == b.EndpointNamespaceKind && a.CustomDomainID == b.CustomDomainID &&
		a.AttestorNamespace == b.AttestorNamespace && a.AttestorServiceAccount == b.AttestorServiceAccount && bytes.Equal(a.ProviderResources, b.ProviderResources)
}

func providerFailure(err error, code string) *ReconcileError {
	switch {
	case errors.Is(err, k8s.ErrNetworkIngressDeletionPending):
		return reconcileFailure("deletion_pending")
	case errors.Is(err, k8s.ErrNetworkIngressReplacementPending):
		return reconcileFailure("replacement_pending")
	case errors.Is(err, k8s.ErrNetworkIngressInvalidDesiredState):
		return reconcileFailure("invalid_desired_state")
	case errors.Is(err, k8s.ErrNetworkIngressUnsupportedProvider):
		return reconcileFailure("unsupported_provider")
	}
	switch code {
	case "invalid_desired_state", "unsupported_provider", "invalid_credentials", "kubernetes_api":
		return reconcileFailure(code)
	default:
		return reconcileFailure("provider_error")
	}
}

func reconcileFailure(code string) *ReconcileError {
	retryable := true
	switch code {
	case "invalid_desired_state", "unsupported_provider", "invalid_credentials", "provider_configuration_unavailable":
		retryable = false
	}
	return &ReconcileError{Code: code, Retryable: retryable}
}
