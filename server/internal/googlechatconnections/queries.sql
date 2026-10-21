-- name: ListConnections :many
SELECT * FROM google_chat_connections WHERE organization_id = @organization_id ORDER BY created_at;
-- name: GetConnection :one
SELECT * FROM google_chat_connections WHERE organization_id = @organization_id AND id = @id;
-- name: SaveConnection :one
INSERT INTO google_chat_connections (organization_id,google_subject,email,credentials_encrypted,granted_scopes)
VALUES (@organization_id,@google_subject,@email,@credentials_encrypted,@granted_scopes)
ON CONFLICT (organization_id,google_subject) DO UPDATE SET email=excluded.email,credentials_encrypted=excluded.credentials_encrypted,granted_scopes=excluded.granted_scopes,generation=generate_uuidv7(),status='connected',updated_at=clock_timestamp()
RETURNING *;
-- name: DisconnectConnection :one
UPDATE google_chat_connections SET credentials_encrypted=NULL,status='disconnected',generation=generate_uuidv7(),updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @generation RETURNING *;
-- name: ListBindings :many
SELECT * FROM google_chat_bindings WHERE organization_id = @organization_id AND status <> 'deleted' ORDER BY created_at;
-- name: GetBinding :one
SELECT * FROM google_chat_bindings WHERE organization_id = @organization_id AND id = @id;
-- name: CreateBinding :one
INSERT INTO google_chat_bindings (id,organization_id,project_id,connection_id,assistant_id,space_name,display_name,prefix)
SELECT @id,@organization_id,a.project_id,@connection_id,a.id,@space_name,@display_name,@prefix
FROM assistants a JOIN projects p ON p.id=a.project_id
WHERE a.id = @assistant_id AND a.project_id = @project_id AND a.organization_id = @organization_id AND a.deleted IS FALSE
RETURNING *;
-- name: UpdateBinding :one
UPDATE google_chat_bindings SET trigger_id = sqlc.narg(trigger_id),subscription_name = sqlc.narg(subscription_name),status = @status,last_error = sqlc.narg(last_error),revision=revision+1,updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @id AND revision = @revision RETURNING *;
-- name: ListMembers :many
SELECT * FROM google_chat_members WHERE organization_id = @organization_id AND binding_id = @binding_id ORDER BY display_name,google_user_name;
-- name: ObserveMember :exec
INSERT INTO google_chat_members (organization_id,project_id,binding_id,google_user_name,display_name,email,active)
VALUES (@organization_id,@project_id,@binding_id,@google_user_name,@display_name,@email,true)
ON CONFLICT (binding_id,google_user_name) DO UPDATE SET display_name=excluded.display_name,email=excluded.email,active=true,updated_at=clock_timestamp();
-- name: MarkMembersAbsent :exec
UPDATE google_chat_members SET active=false,updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND binding_id = @binding_id AND NOT (google_user_name=ANY(@present_users::text[]));
-- name: MapMember :one
UPDATE google_chat_members m SET user_id = sqlc.narg(user_id),revision=revision+1,updated_at=clock_timestamp()
WHERE m.organization_id = @organization_id AND m.project_id = @project_id AND m.id = @id AND m.revision = @revision AND m.active
AND (sqlc.narg(user_id)::text IS NULL OR EXISTS (SELECT 1 FROM users u JOIN organization_user_relationships r ON r.user_id=u.id WHERE u.id = sqlc.narg(user_id) AND r.organization_id = @organization_id AND u.deleted_at IS NULL AND r.deleted_at IS NULL))
RETURNING *;
-- name: ResolveMappedUser :one
SELECT m.user_id::text FROM google_chat_members m
JOIN google_chat_bindings b ON b.id=m.binding_id AND b.organization_id=m.organization_id AND b.project_id=m.project_id
JOIN google_chat_connections c ON c.id=b.connection_id AND c.organization_id=b.organization_id
JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL
JOIN organization_user_relationships r ON r.user_id=u.id AND r.organization_id=m.organization_id AND r.deleted_at IS NULL
WHERE m.organization_id = @organization_id AND m.project_id = @project_id AND b.id = @binding_id
AND b.assistant_id = @assistant_id AND m.google_user_name = @google_user_name AND m.active AND b.status='active' AND c.status='connected';
-- name: ResolveChatBinding :one
SELECT b.* FROM google_chat_bindings b JOIN assistant_threads t ON t.assistant_id=b.assistant_id AND t.project_id=b.project_id
JOIN google_chat_connections c ON c.id=b.connection_id AND c.organization_id=b.organization_id
WHERE t.chat_id = @chat_id AND t.deleted IS FALSE AND b.status='active' AND c.status='connected' AND b.space_name = @space_name;
-- name: ListRenewableBindings :many
SELECT b.* FROM google_chat_bindings b JOIN google_chat_connections c ON c.id=b.connection_id AND c.organization_id=b.organization_id
WHERE b.status='active' AND c.status='connected' AND b.id > @after_id ORDER BY b.id LIMIT @page_size;
-- name: GetManagedBinding :one
SELECT b.* FROM google_chat_bindings b JOIN google_chat_connections c ON c.id=b.connection_id AND c.organization_id=b.organization_id
WHERE b.id = @id AND b.project_id = @project_id AND b.status='active' AND c.status='connected';
-- name: UpdateBindingHealth :exec
UPDATE google_chat_bindings SET last_error=sqlc.narg(last_error),updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @id AND status='active';
-- name: DisconnectBindings :many
UPDATE google_chat_bindings SET status='deleted',revision=revision+1,updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND connection_id = @connection_id AND status<>'deleted' RETURNING *;

-- name: AutoMapMembers :many
WITH candidates AS (
 SELECT m.id,min(u.id)::text AS user_id FROM google_chat_members m
 JOIN organization_user_relationships r ON r.organization_id=m.organization_id AND r.deleted_at IS NULL
 JOIN users u ON u.id=r.user_id AND u.deleted_at IS NULL AND lower(trim(u.email))=lower(trim(m.email))
 WHERE m.organization_id = @organization_id AND m.project_id = @project_id AND m.binding_id = @binding_id
 AND m.active AND m.revision=0 AND m.user_id IS NULL AND trim(m.email)<>''
 AND NOT EXISTS (SELECT 1 FROM google_chat_members other WHERE other.binding_id=m.binding_id AND other.id<>m.id AND lower(trim(other.email))=lower(trim(m.email)))
 GROUP BY m.id HAVING count(*)=1
)
UPDATE google_chat_members m SET user_id=c.user_id,revision=1,updated_at=clock_timestamp() FROM candidates c WHERE m.id=c.id RETURNING m.*;

-- name: SetBindingSpace :one
UPDATE google_chat_bindings SET space_name = @space_name,display_name = @display_name,updated_at=clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @id AND status IN ('pending','error') RETURNING *;

-- name: RecordPostedThreadRoute :exec
-- The first invocation or assistant post owns a Chat thread's conversation.
INSERT INTO trigger_thread_routes (project_id,target_kind,target_ref,correlation_id,route_to_correlation_id,state,last_seen_cursor)
SELECT b.project_id,'assistant',b.assistant_id::text,'googlechat:' || b.trigger_id::text || ':' || @thread_name::text,t.correlation_id,'subscribed',NULL
FROM google_chat_bindings b
JOIN assistant_threads t ON t.project_id=b.project_id AND t.assistant_id=b.assistant_id AND t.deleted IS FALSE
JOIN google_chat_connections c ON c.id=b.connection_id AND c.organization_id=b.organization_id
WHERE b.id = @binding_id AND t.chat_id = @chat_id AND b.status='active' AND b.trigger_id IS NOT NULL AND c.status='connected'
ON CONFLICT (project_id,target_kind,target_ref,correlation_id) WHERE deleted IS FALSE DO NOTHING;
