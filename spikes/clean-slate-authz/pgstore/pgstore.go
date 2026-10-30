// Package pgstore is the Postgres authz.Store: one relationship table, a
// per-organization revision bumped by trigger, and a recursive subject loader.
package pgstore

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/model"
)

//go:embed schema.sql
var ddl string

type Store struct {
	Pool *pgxpool.Pool
}

// Reset recreates the spike tables.
func (s *Store) Reset(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, ddl)
	return err
}

func (s *Store) Seed(ctx context.Context, org string, rows []authz.Relationship, agents []authz.AgentRow, keys []authz.APIKeyRow) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, a := range agents {
		if _, err := tx.Exec(ctx, `INSERT INTO spike_agents VALUES ($1,$2,$3,$4,$5)`, a.ID, org, a.ProjectID, a.OwnerUserID, a.Status); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if _, err := tx.Exec(ctx, `INSERT INTO spike_api_keys VALUES ($1,$2,$3,$4,$5)`, k.ID, org, k.AgentID, k.RevokedAt, k.ExpiresAt); err != nil {
			return err
		}
	}
	if err := s.insert(ctx, tx, org, rows); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Write inserts relationships inside the caller's transaction, so a grant and
// the domain change that caused it commit (or roll back) together.
func (s *Store) Write(ctx context.Context, tx pgx.Tx, org string, rows ...authz.Relationship) error {
	return s.insert(ctx, tx, org, rows)
}

func (s *Store) insert(ctx context.Context, tx pgx.Tx, org string, rows []authz.Relationship) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		var cond *string
		var params []byte
		if r.ConditionName != "" {
			cond, params = &r.ConditionName, r.ConditionParams
		}
		b.Queue(`INSERT INTO authz_relationships
			(id, organization_id, object_type, object_id, relation, subject_type, subject_id, subject_relation,
			 condition_name, condition_params, expires_at, source, source_ref)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''))`,
			r.ID, org, r.Object.Type, r.Object.ID, r.Relation, r.Subject.Type, r.Subject.ID, r.Subject.Relation,
			cond, params, r.ExpiresAt, r.Source, r.SourceRef)
	}
	return tx.SendBatch(ctx, b).Close()
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM authz_relationships WHERE id = $1`, id)
	return err
}

func (s *Store) Revision(ctx context.Context, org string) (int64, error) {
	var rev int64
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM authz_revisions WHERE organization_id = $1), 0)`, org).Scan(&rev)
	return rev, err
}

// Begin opens the consistent read admission needs: the revision, live domain
// rows and relationships all come from one MVCC snapshot on the primary.
func (s *Store) Begin(ctx context.Context, org string) (authz.Tx, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	t := &pgTx{tx: tx, org: org}
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM authz_revisions WHERE organization_id = $1), 0), now()`, org).Scan(&t.revision, &t.now)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return t, nil
}

type pgTx struct {
	tx       pgx.Tx
	org      string
	revision int64
	now      time.Time
}

func (t *pgTx) Revision() int64                 { return t.revision }
func (t *pgTx) Now() time.Time                  { return t.now }
func (t *pgTx) Close(ctx context.Context) error { return t.tx.Rollback(ctx) }

func (t *pgTx) APIKey(ctx context.Context, id string) (authz.APIKeyRow, error) {
	k := authz.APIKeyRow{ID: id}
	err := t.tx.QueryRow(ctx, `SELECT agent_id, revoked_at, expires_at FROM spike_api_keys WHERE id = $1 AND organization_id = $2`, id, t.org).
		Scan(&k.AgentID, &k.RevokedAt, &k.ExpiresAt)
	return k, err
}

func (t *pgTx) Agent(ctx context.Context, id string) (authz.AgentRow, error) {
	a := authz.AgentRow{ID: id}
	err := t.tx.QueryRow(ctx, `SELECT owner_user_id, project_id, status FROM spike_agents WHERE id = $1 AND organization_id = $2`, id, t.org).
		Scan(&a.OwnerUserID, &a.ProjectID, &a.Status)
	return a, err
}

const loadSubject = `
WITH RECURSIVE closure(subject_type, subject_id, subject_relation, depth) AS (
  SELECT $2::text, $3::text, ''::text, 0
  UNION
  SELECT r.object_type, r.object_id, r.relation, c.depth + 1
  FROM authz_relationships r
  JOIN closure c USING (subject_type, subject_id, subject_relation)
  WHERE r.organization_id = $1
    AND r.relation = ANY($4::text[])
    AND (r.expires_at IS NULL OR r.expires_at > now())
    AND c.depth < 3
)
SELECT r.id, r.object_type, r.object_id, r.relation, r.subject_type, r.subject_id, r.subject_relation,
       COALESCE(r.condition_name, ''), r.condition_params, r.expires_at, r.source, COALESCE(r.source_ref, '')
FROM authz_relationships r
JOIN (SELECT DISTINCT subject_type, subject_id, subject_relation FROM closure) c
  USING (subject_type, subject_id, subject_relation)
WHERE r.organization_id = $1
  AND (r.expires_at IS NULL OR r.expires_at > now())`

func (t *pgTx) SubjectRelationships(ctx context.Context, s authz.Subject) ([]authz.Relationship, error) {
	rows, err := t.tx.Query(ctx, loadSubject, t.org, s.Type, s.ID, model.MembershipRelations)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

func (t *pgTx) OrgFacts(ctx context.Context, markers []string, _ []string) (memberships, marked []authz.Relationship, err error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, object_type, object_id, relation, subject_type, subject_id, subject_relation,
		       COALESCE(condition_name, ''), condition_params, expires_at, source, COALESCE(source_ref, '')
		FROM authz_relationships
		WHERE organization_id = $1
		  AND ((object_type = 'group' AND relation = 'member' AND subject_type = 'user') OR relation = ANY($2::text[]))
		  AND (expires_at IS NULL OR expires_at > now())`, t.org, markers)
	if err != nil {
		return nil, nil, err
	}
	all, err := scan(rows)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range all {
		if r.Relation == "member" {
			memberships = append(memberships, r)
		} else {
			marked = append(marked, r)
		}
	}
	return memberships, marked, nil
}

func scan(rows pgx.Rows) ([]authz.Relationship, error) {
	defer rows.Close()
	var out []authz.Relationship
	for rows.Next() {
		var r authz.Relationship
		var params []byte
		if err := rows.Scan(&r.ID, &r.Object.Type, &r.Object.ID, &r.Relation, &r.Subject.Type, &r.Subject.ID, &r.Subject.Relation,
			&r.ConditionName, &params, &r.ExpiresAt, &r.Source, &r.SourceRef); err != nil {
			return nil, err
		}
		if params != nil {
			r.ConditionParams = json.RawMessage(params)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeedChats loads the stand-in chat rows used by the list pushdown demo.
func (s *Store) SeedChats(ctx context.Context, org, project string, chats []struct{ ID, UserID, AgentID string }) error {
	for _, c := range chats {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO spike_chats VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''))`, c.ID, org, project, c.UserID, c.AgentID); err != nil {
			return fmt.Errorf("seed chat %s: %w", c.ID, err)
		}
	}
	return nil
}
