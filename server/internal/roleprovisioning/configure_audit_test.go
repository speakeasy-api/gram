package roleprovisioning_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConfigureIdenticalSaveAuditsNewVersion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	input := roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, Enabled: true, ProjectID: &f.project, Roles: []roleprovisioning.Selection{{RoleURN: f.role, Enabled: true}}}
	v, err := f.service.Configure(t.Context(), input)
	require.NoError(t, err)
	audits := f.configurationAuditCount()
	hints := f.count(`SELECT count(*) FROM publish_outbox WHERE topic='gram.plugins.v1.RoleProvisioningRequested'`)
	outbox := f.count(`SELECT count(*) FROM publish_outbox`)
	input.ExpectedVersion = v
	next, err := f.service.Configure(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, v+1, next)
	require.Equal(t, audits+1, f.configurationAuditCount())
	require.Equal(t, hints+1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic='gram.plugins.v1.RoleProvisioningRequested'`))
	require.Equal(t, outbox+2, f.count(`SELECT count(*) FROM publish_outbox`), "one audit event and one maintenance hint")
	entry := f.latestConfigurationAudit()
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	expected := map[string]any{"enabled": true, "project_id": f.project.String(), "version": float64(v), "roles": []any{map[string]any{"role_urn": f.role, "enabled": true, "project_id": f.project.String()}}}
	require.Equal(t, expected, before)
	expected["version"] = float64(next)
	require.Equal(t, expected, after)
	saved, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, next, saved.Version)
}

func TestConfigureAuditsSavedIntent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.actor.Principal = urn.NewPrincipal(urn.PrincipalTypeUser, "user_config_fixture")
	audits := f.configurationAuditCount()
	v := f.configure(0, true)
	require.Equal(t, audits+1, f.configurationAuditCount())
	f.latestConfigurationAudit()
	f.configure(v, false, roleprovisioning.Selection{RoleURN: f.role, Enabled: false, ProjectID: new(uuid.Nil)})
	require.Equal(t, audits+2, f.configurationAuditCount())
	entry := f.latestConfigurationAudit()
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"enabled": true, "project_id": f.project.String(), "version": float64(v), "roles": []any{map[string]any{"role_urn": f.role, "enabled": true, "project_id": f.project.String()}}}, before)
	require.Equal(t, map[string]any{"enabled": false, "project_id": f.project.String(), "version": float64(v + 1), "roles": []any{map[string]any{"role_urn": f.role, "enabled": false, "project_id": nil}}}, after)
	_, err = f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: v, Enabled: true})
	require.ErrorIs(t, err, roleprovisioning.ErrConflict)
	require.Equal(t, audits+2, f.configurationAuditCount(), "stale intent must not create a misleading audit")
}

func TestConfigureRequiresAuditAndValidActor(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing-logger", "zero-principal", "invalid-principal"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			actor := f.actor
			switch mode {
			case "missing-logger":
				f.service = roleprovisioning.New(f.db, nil, nil, plugins.PublicationRequests{})
			case "zero-principal":
				actor.Principal = urn.Principal{}
			case "invalid-principal":
				actor.Principal = urn.Principal{Type: urn.PrincipalTypeUser}
			}
			_, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: actor, OrganizationID: f.org, Enabled: true})
			require.ErrorIs(t, err, roleprovisioning.ErrInvalid)
			settings, err := f.service.Settings(t.Context(), f.org)
			require.NoError(t, err)
			require.Zero(t, settings.Version)
			require.Empty(t, settings.Roles)
			require.Zero(t, f.configurationAuditCount())
			require.Zero(t, f.count(`SELECT count(*) FROM publish_outbox`))
		})
	}
}

func TestConfigureAuditAndHintFailuresRollBackTogether(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"audit", "hint"} {
		for _, existing := range []bool{false, true} {
			name := target + map[bool]string{false: "/initial", true: "/update"}[existing]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := newFixture(t)
				if existing {
					f.configure(0, true)
				}
				before, err := f.service.Settings(t.Context(), f.org)
				require.NoError(t, err)
				audits := f.configurationAuditCount()
				outbox := f.count(`SELECT count(*) FROM publish_outbox`)
				if target == "audit" {
					f.exec(`CREATE FUNCTION reject_configuration_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected configuration audit failure'; END $$`)
					f.exec(`CREATE TRIGGER reject_configuration_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_configuration_audit()`)
				} else {
					f.exec(`CREATE FUNCTION reject_configuration_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='gram.plugins.v1.RoleProvisioningRequested' THEN RAISE EXCEPTION 'injected configuration hint failure'; END IF; RETURN NEW; END $$`)
					f.exec(`CREATE TRIGGER reject_configuration_hint BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_configuration_hint()`)
				}
				_, err = f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: before.Version, Enabled: !before.Enabled, ProjectID: new(uuid.Nil), Roles: []roleprovisioning.Selection{{RoleURN: f.role, Enabled: false}}})
				require.ErrorContains(t, err, "injected configuration "+target+" failure")
				after, err := f.service.Settings(t.Context(), f.org)
				require.NoError(t, err)
				require.Equal(t, before, after, "settings, selections and version must roll back")
				require.Equal(t, audits, f.configurationAuditCount())
				require.Equal(t, outbox, f.count(`SELECT count(*) FROM publish_outbox`), "audit event and maintenance hint must roll back")
			})
		}
	}
}

func (f *fixture) configurationAuditCount() int64 {
	f.t.Helper()
	count, err := audittest.AuditLogCountByAction(f.t.Context(), f.db, audit.ActionOrganizationRoleProvisioningConfigured)
	require.NoError(f.t, err)
	return count
}

func (f *fixture) latestConfigurationAudit() audittest.LogRecord {
	f.t.Helper()
	entry, err := audittest.LatestAuditLogByAction(f.t.Context(), f.db, audit.ActionOrganizationRoleProvisioningConfigured)
	require.NoError(f.t, err)
	require.Equal(f.t, f.org, entry.OrganizationID)
	require.False(f.t, entry.ProjectID.Valid)
	require.Equal(f.t, string(f.actor.Principal.Type), entry.ActorType)
	require.Equal(f.t, f.actor.Principal.ID, entry.ActorID)
	require.Equal(f.t, "organization", entry.SubjectType)
	require.Equal(f.t, f.org, entry.SubjectID)
	return entry
}
