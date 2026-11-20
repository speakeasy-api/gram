-- name: CreateOrganization :execrows
INSERT INTO organization_metadata(id,name,slug) VALUES($1,'Example',$1);

-- name: CreateProject :execrows
INSERT INTO projects(id,name,slug,organization_id) VALUES($1,'Example','example',$2);

-- name: CreateChat :execrows
INSERT INTO chats(id,project_id,organization_id) VALUES($1,$2,$3);

-- name: CreateMessage :execrows
INSERT INTO chat_messages(id,chat_id,project_id,role,content,content_raw,tool_calls,tool_outcome_notes,tool_call_summaries) VALUES($1,$2,$3,'assistant','sensitive example','{"large":9007199254740993,"n":1e2}','[{"id":"example"}]','example note','{"example":"summary"}');

-- name: ChangeContent :execrows
UPDATE chat_messages SET content='changed' WHERE id=$1 AND project_id=$2;

-- name: ForgeSourceHash :execrows
UPDATE chat_message_transcripts t SET source_hash=sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')) FROM chat_messages m WHERE m.id=t.message_id AND m.id=$1 AND m.project_id=$2 AND t.project_id=$2 AND t.organization_id=$3;

-- name: ReplaceCiphertext :execrows
UPDATE chat_message_transcripts SET ciphertext=$1 WHERE message_id=$2 AND project_id=$3 AND organization_id=$4;

-- name: ChangeNotes :execrows
UPDATE chat_messages SET tool_outcome_notes='changed' WHERE id=$1 AND project_id=$2;

-- name: RaceContent :execrows
UPDATE chat_messages SET content='raced' WHERE id=$1 AND project_id=$2;

-- name: UnattributeMessage :execrows
UPDATE chat_messages SET project_id=NULL WHERE id=$1 AND project_id=$2;

-- name: RaceRawContent :execrows
UPDATE chat_messages SET content_raw='{"different":true}' WHERE id=$1 AND project_id=$2;

-- name: CreateOtherProject :execrows
INSERT INTO projects(id,name,slug,organization_id) VALUES($1,'Other','other',$2);

-- name: MoveMessageProject :execrows
UPDATE chat_messages SET project_id=$1 WHERE id=$2 AND project_id=$3;

-- name: MessageSnapshot :one
SELECT row_to_json(m)::text FROM chat_messages m WHERE id=$1 AND project_id=$2;

-- name: Ciphertext :one
SELECT ciphertext FROM chat_message_transcripts WHERE message_id=$1 AND project_id=$2 AND organization_id=$3;

-- name: SetJSONField :execrows
UPDATE chat_messages SET
content_raw=CASE WHEN $1::text='content_raw' THEN $2::jsonb ELSE content_raw END,
tool_calls=CASE WHEN $1::text='tool_calls' THEN $2::jsonb ELSE tool_calls END,
tool_call_summaries=CASE WHEN $1::text='tool_call_summaries' THEN $2::jsonb ELSE tool_call_summaries END
WHERE id=$3 AND project_id=$4;

-- name: LockChat :exec
SELECT id FROM chats WHERE id=$1 AND project_id=$2 AND organization_id=$3 FOR UPDATE;

-- name: SetStorageError :execrows
UPDATE chat_messages SET storage_error=sqlc.narg(storage_error)::text WHERE id=sqlc.arg(message_id) AND project_id=sqlc.arg(project_id);
