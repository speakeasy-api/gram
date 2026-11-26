-- name: CreateOrganization :execrows
INSERT INTO organization_metadata(id,name,slug) VALUES($1,'Example',$1);

-- name: CreateProject :execrows
INSERT INTO projects(id,name,slug,organization_id) VALUES($1,'Example','example',$2);

-- name: CreateChat :execrows
INSERT INTO chats(id,project_id,organization_id) VALUES($1,$2,$3);

-- name: CreateMessage :execrows
INSERT INTO chat_messages(id,chat_id,project_id,role,content,content_raw,tool_calls,tool_outcome_notes,tool_call_summaries) VALUES($1,$2,$3,'assistant','sensitive example','{"large":9007199254740993,"n":1e2}','[{"id":"example"}]','example note','{"example":"summary"}');

-- Body updates stamp source_updated_at as the application's writes do.
-- name: ChangeContent :execrows
UPDATE chat_messages SET content='changed', source_updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2;

-- name: ForgeSourceUpdatedAt :execrows
UPDATE chat_message_transcripts t SET source_updated_at=m.source_updated_at, source_hash=NULL FROM chat_messages m WHERE m.id=t.message_id AND m.id=$1 AND m.project_id=$2 AND t.project_id=$2 AND t.organization_id=$3;

-- name: ReplaceCiphertext :execrows
UPDATE chat_message_transcripts SET ciphertext=$1 WHERE message_id=$2 AND project_id=$3 AND organization_id=$4;

-- name: ChangeNotes :execrows
UPDATE chat_messages SET tool_outcome_notes='changed', source_updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2;

-- name: RaceContent :execrows
UPDATE chat_messages SET content='raced', source_updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2;

-- name: UnattributeMessage :execrows
UPDATE chat_messages SET project_id=NULL WHERE id=$1 AND project_id=$2;

-- name: RaceRawContent :execrows
UPDATE chat_messages SET content_raw='{"different":true}', source_updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2;

-- name: CreateOtherProject :execrows
INSERT INTO projects(id,name,slug,organization_id) VALUES($1,'Other','other',$2);

-- name: MoveMessageProject :execrows
UPDATE chat_messages SET project_id=$1 WHERE id=$2 AND project_id=$3;

-- name: MessageSnapshot :one
-- The message as sealing must leave it: the metadata columns are the only ones
-- it writes.
SELECT (to_jsonb(m) - 'tool_call_count' - 'content_bytes' - 'content_raw_bytes' - 'tool_calls_bytes')::text FROM chat_messages m WHERE id=$1 AND project_id=$2;

-- name: Ciphertext :one
SELECT ciphertext FROM chat_message_transcripts WHERE message_id=$1 AND project_id=$2 AND organization_id=$3;

-- name: SetJSONField :execrows
UPDATE chat_messages SET
content_raw=CASE WHEN $1::text='content_raw' THEN $2::jsonb ELSE content_raw END,
tool_calls=CASE WHEN $1::text='tool_calls' THEN $2::jsonb ELSE tool_calls END,
tool_call_summaries=CASE WHEN $1::text='tool_call_summaries' THEN $2::jsonb ELSE tool_call_summaries END, source_updated_at=clock_timestamp()
WHERE id=$3 AND project_id=$4;

-- name: LockChat :exec
SELECT id FROM chats WHERE id=$1 AND project_id=$2 AND organization_id=$3 FOR UPDATE;

-- name: SetStorageError :execrows
UPDATE chat_messages SET storage_error=sqlc.narg(storage_error)::text, source_updated_at=clock_timestamp() WHERE id=sqlc.arg(message_id) AND project_id=sqlc.arg(project_id);

-- name: InsertShadow :execrows
INSERT INTO chat_message_transcripts(message_id,project_id,organization_id,ciphertext,source_hash)
VALUES($1,$2,$3,'test ciphertext',decode(repeat('00',32),'hex'));

-- name: ShadowMetadata :one
SELECT tool_call_count, content_bytes, content_raw_bytes, tool_calls_bytes FROM chat_messages WHERE id=$1 AND tool_call_count IS NOT NULL;

-- name: ShadowToolCallIDs :many
SELECT tool_call_id FROM chat_message_tool_call_ids WHERE message_id=$1 ORDER BY tool_call_id;

-- name: SetToolCalls :execrows
UPDATE chat_messages SET tool_calls=$1, source_updated_at=clock_timestamp() WHERE id=$2 AND project_id=$3;

-- name: SetShadowMetadata :execrows
UPDATE chat_messages SET tool_call_count=sqlc.narg(tool_call_count), content_bytes=sqlc.narg(content_bytes), content_raw_bytes=sqlc.narg(content_raw_bytes), tool_calls_bytes=sqlc.narg(tool_calls_bytes) WHERE id=sqlc.arg(message_id);

-- name: SetRoleAndSource :execrows
UPDATE chat_messages SET role=sqlc.arg(role), source=sqlc.arg(source) WHERE id=sqlc.arg(message_id) AND project_id=sqlc.arg(project_id);

-- name: SetExternalMessageID :execrows
UPDATE chat_messages SET external_message_id=sqlc.arg(external_message_id), source=sqlc.arg(source) WHERE id=sqlc.arg(message_id) AND project_id=sqlc.arg(project_id);

-- name: RiskAnalyzedAt :one
SELECT risk_analyzed_at FROM chat_messages WHERE id=$1;

-- name: MarkRiskAnalyzed :execrows
UPDATE chat_messages SET risk_analyzed_at=clock_timestamp() WHERE id=$1;

-- name: SetSource :execrows
UPDATE chat_messages SET source=$1 WHERE id=$2;

-- name: MessageFreshness :one
SELECT source_updated_at, tool_call_count FROM chat_messages WHERE id=$1;

-- name: MessageMetadata :one
SELECT tool_call_count, content_bytes, content_raw_bytes, tool_calls_bytes FROM chat_messages WHERE id=$1;

-- name: MakeLegacyCopy :execrows
-- Turn a sealed copy back into one sealed before source_updated_at existed:
-- no recorded timestamp, and the unkeyed source_hash of the current row.
UPDATE chat_message_transcripts t SET source_updated_at=NULL, source_hash=sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')) FROM chat_messages m WHERE m.id=t.message_id AND m.id=$1 AND m.project_id=$2 AND t.project_id=$2;

-- name: CopySourceHashIsSet :one
SELECT (source_hash IS NOT NULL)::boolean AS hashed FROM chat_message_transcripts WHERE message_id=$1;
