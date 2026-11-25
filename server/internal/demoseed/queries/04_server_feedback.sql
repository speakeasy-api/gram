-- Agent feedback sent through MCP servers' built-in feedback tool
-- (seed_04_server_feedback.go). Both tables sit inside the demo project's
-- delete cascade, so the purge in the org phase clears them before each rerun.

-- name: ServerFeedbackInsertConfig :exec
INSERT INTO mcp_server_feedback_configs (mcp_server_id, organization_id, project_id,
                                         enabled, tool_name, updated_by_user_id,
                                         created_at, updated_at)
VALUES (@mcp_server_id::uuid, @org_id::text, @project_id::uuid,
        @enabled::boolean, sqlc.narg(tool_name)::text, @updated_by_user_id::text,
        now() - make_interval(days => @changed_days_ago::integer),
        now() - make_interval(days => @changed_days_ago::integer));

-- name: ServerFeedbackInsertReport :exec
INSERT INTO mcp_server_feedback (id, organization_id, project_id, mcp_server_id,
                                 content, severity, workaround, intention,
                                 tool_name, model, user_id,
                                 external_user_id, client_name, client_version,
                                 created_at, updated_at)
VALUES (@id::uuid, @org_id::text, @project_id::uuid, @mcp_server_id::uuid,
        @content::text, sqlc.narg(severity)::text, sqlc.narg(workaround)::text,
        sqlc.narg(intention)::text, sqlc.narg(tool_name)::text, sqlc.narg(model)::text,
        sqlc.narg(user_id)::text, sqlc.narg(external_user_id)::text,
        sqlc.narg(client_name)::text, sqlc.narg(client_version)::text,
        now() - make_interval(mins => @minutes_ago::integer),
        now() - make_interval(mins => @minutes_ago::integer));

-- name: ServerFeedbackCountConfigs :one
SELECT count(*) FROM mcp_server_feedback_configs WHERE project_id = @project_id::uuid;

-- name: ServerFeedbackCountReports :one
SELECT count(*) FROM mcp_server_feedback WHERE project_id = @project_id::uuid;
