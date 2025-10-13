package toolmetrics

var insertHttpRaw = `insert into gram.http_requests_raw
    (id, ts, organization_id, project_id, deployment_id, tool_id, tool_urn, tool_type, trace_id, span_id, http_method,
     http_route, status_code, duration_ms, user_agent, request_headers, request_body_bytes, response_headers, response_body_bytes) 
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`

var listLogsQueryDesc = `
select * from gram.http_requests_raw
where project_id = $1
and ts >= $2
and ts <= $3
and ts < $4
order by ts desc
limit $5
`

var listLogsQueryAsc = `
select * from gram.http_requests_raw
where project_id = $1
and ts >= $2
and ts <= $3
and ts > $4
order by ts
limit $5
`
