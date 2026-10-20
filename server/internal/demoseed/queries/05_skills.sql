-- Prompts (the Prompts page otherwise falls back to onboarding).
-- engine must be 'mustache': the dashboard SDK's Engine enum is closed and
-- a NULL/empty engine fails client-side validation, crashing every page
-- that embeds a prompt template (Prompts list, source detail via
-- tools.list). URN kind is 'prompt', not the http tools' kind.
-- name: SkillsInsertPromptTemplates :exec
INSERT INTO prompt_templates (id, tool_urn, project_id, history_id, name, description, prompt, engine, kind)
VALUES
  (@triage_id::uuid, 'tools:prompt:acme:triage-ticket', @project_id::uuid,
   @triage_history_id::uuid, 'triage-ticket',
   'Structured first response for a new support ticket.',
   'Read the customer message, identify the order id, check recent errors with search_logs, and draft a first response.',
   'mustache', 'prompt'),
  (@latency_id::uuid, 'tools:prompt:acme:latency-investigation', @project_id::uuid,
   @latency_history_id::uuid, 'latency-investigation',
   'Investigate a latency regression end to end.',
   'Fetch traces for the affected service, compare p95 against the last deploy, and summarize the likely cause.',
   'mustache', 'prompt');

-- name: SkillsInsertSkill :exec
INSERT INTO skills (id, project_id, name, display_name, summary, source_kind,
                    classification, first_seen_at, last_seen_at, seen_count)
VALUES (@id::uuid, @project_id::uuid, @name::text, @display_name::text, @summary::text,
        'manual', 'custom', now() - interval '10 days', now() - interval '1 day', @seen_count::bigint);

-- Version shas are display-only on the read path, so sha256(content) stands
-- in for the server's canonicalized sha.
-- name: SkillsInsertInitialVersion :exec
INSERT INTO skill_versions (id, skill_id, content, canonical_sha256, raw_sha256,
                            description, spec_valid, created_by_user_id, created_at)
SELECT @id::uuid, @skill_id::uuid, c,
       encode(sha256(convert_to(c, 'UTF8')), 'hex'),
       encode(sha256(convert_to(c, 'UTF8')), 'hex'),
       'Initial version', TRUE, @created_by_user_id::text, now() - interval '10 days'
FROM (SELECT @content::text AS c) s;

-- name: SkillsInsertEditSuggestion :exec
INSERT INTO skill_edit_suggestions (id, project_id, skill_id, base_version_id,
                                    rationale, status, scored_session_count)
VALUES (@id::uuid, @project_id::uuid,
        @skill_id::uuid, @base_version_id::uuid,
        'Sessions show agents accepting card numbers in chat before refusing; the skill should forbid it up front.',
        'open', 7);

-- The diff MUST carry ---/+++ file headers: gitdiff.Parse yields zero file
-- fragments for a bare hunk, skilldiff.Apply then fails and the dashboard
-- retires the suggestion as "no longer lines up with the manifest".
-- name: SkillsInsertEditSuggestionChange :exec
INSERT INTO skill_edit_suggestion_changes (project_id, suggestion_id, proposed_diff, rationale, position)
VALUES
  (@project_id::uuid, @suggestion_id::uuid,
E'--- a/SKILL.md\n+++ b/SKILL.md\n@@ -6,4 +6,5 @@\n # Refund handling\n \n 1. Verify the order id and amount with the customer.\n+1a. If the customer pastes a card number, stop and ask them to remove it.\n 2. Use the process_refund tool with the confirmed amount.',
   'Make the PCI guardrail the first checkpoint instead of a trailing note.', 1);

-- Runbook v2: the base runbook content plus an on-call paging step.
-- name: SkillsInsertRunbookV2 :exec
INSERT INTO skill_versions (id, skill_id, content, canonical_sha256, raw_sha256,
                            description, spec_valid, created_by_user_id, created_at, promoted_at)
SELECT @id::uuid, @skill_id::uuid, c,
       encode(sha256(convert_to(c, 'UTF8')), 'hex'),
       encode(sha256(convert_to(c, 'UTF8')), 'hex'),
       'Add on-call paging step', TRUE, @created_by_user_id::text,
       now() - interval '3 days', now() - interval '3 days'
FROM (SELECT @content::text AS c) s;

-- name: SkillsInsertVersionLineage :exec
INSERT INTO skill_version_lineages (skill_version_id, skill_id, derived_from_version_id)
VALUES (@skill_version_id::uuid, @skill_id::uuid, @derived_from_version_id::uuid);

-- One active plugin distribution per skill.
-- name: SkillsInsertDistributions :exec
INSERT INTO skill_distributions (id, project_id, skill_id, channel, created_by_user_id, created_at)
VALUES
  (@id1::uuid, @project_id::uuid, @skill_id1::uuid,
   'plugin', @created_by_user_id::text, now() - interval '9 days'),
  (@id2::uuid, @project_id::uuid, @skill_id2::uuid,
   'plugin', @created_by_user_id::text, now() - interval '9 days'),
  (@id3::uuid, @project_id::uuid, @skill_id3::uuid,
   'plugin', @created_by_user_id::text, now() - interval '9 days');

-- A reconciled activation on a bulk chat. raw_sha256 is the resolved
-- version's canonical sha.
-- name: SkillsInsertActivation :exec
INSERT INTO skill_observations (id, project_id, idempotency_key, provider,
  user_id, user_email, hostname, session_id, skill_name, source,
  source_level, source_path, raw_sha256, seen_at, skill_id,
  skill_version_id, reconciled_at, metrics_synced_at, efficacy_enqueued_at)
VALUES (@id::uuid, @project_id::uuid,
  @idempotency_key::text, 'claude-code',
  @user_id::text, @user_email::text,
  @hostname::text,
  @session_id::text,
  @skill_name::text, 'plugin', 'project',
  '.claude/skills/' || @skill_name::text || '/SKILL.md',
  (SELECT canonical_sha256 FROM skill_versions WHERE id = @skill_version_id::uuid),
  @seen_at::timestamptz,
  @skill_id::uuid, @skill_version_id::uuid,
  @reconciled_at::timestamptz, @synced_at::timestamptz,
  @synced_at::timestamptz);

-- A runbook drift observation: the latest one per machine decides its state.
-- raw_sha256 comes from sha_version_id; skill_version_id is NULL for the
-- indeterminate machine.
-- name: SkillsInsertDriftObservation :exec
INSERT INTO skill_observations (id, project_id, idempotency_key, provider,
  user_id, user_email, hostname, skill_name, source, raw_sha256, seen_at,
  skill_id, skill_version_id, reconciled_at, metrics_synced_at, efficacy_enqueued_at)
VALUES (@id::uuid, @project_id::uuid,
  @idempotency_key::text, 'claude-code',
  @user_id::text, @user_email::text, @hostname::text,
  'runbook', 'plugin',
  (SELECT canonical_sha256 FROM skill_versions WHERE id = @sha_version_id::uuid),
  @seen_at::timestamptz,
  @skill_id::uuid,
  sqlc.narg(skill_version_id)::uuid,
  now() - interval '110 minutes', now() - interval '109 minutes',
  now() - interval '109 minutes');

-- Unknown activations (Skills list footer): reconciled with error codes.
-- name: SkillsInsertUnknownActivations :exec
INSERT INTO skill_observations (id, project_id, idempotency_key, provider,
  user_id, user_email, hostname, skill_name, source, raw_sha256, seen_at,
  reconciled_at, reconcile_error_code)
VALUES
  (@id1::uuid, @project_id::uuid, 'demo-skillobs-unknown-1',
   'claude-code', @user_id1::text, @user_email1::text, @hostname1::text,
   'Deploy Checklist!!', 'plugin', NULL,
   now() - interval '30 hours', now() - interval '30 hours', 'invalid_name'),
  (@id2::uuid, @project_id::uuid, 'demo-skillobs-unknown-2',
   'claude-code', @user_id2::text, @user_email2::text, @hostname2::text,
   'db-failover', 'plugin', encode(sha256(convert_to('demo-unknown-manifest', 'UTF8')), 'hex'),
   now() - interval '8 hours', now() - interval '8 hours', 'unresolved_hash');
