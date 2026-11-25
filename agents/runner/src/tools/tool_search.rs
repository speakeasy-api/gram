use agentkit_core::{ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    CatalogReader, Tool, ToolContext, ToolError, ToolRequest, ToolResult, ToolSource, ToolSpec,
};
use std::collections::BTreeSet;
use std::sync::atomic::Ordering;
use std::sync::{Mutex, PoisonError};

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::{Value, json};
use tokio::sync::{mpsc, oneshot};

use crate::compaction::TranscriptEpoch;
use crate::mcp_actor::{McpCmd, McpServerStatus};

pub const TOOL_NAME: &str = "tool_search";

/// Full schemas returned per search. The compact name index always covers
/// the whole catalog, so a miss costs one more search, not a blind spot.
const MAX_MATCHES: usize = 8;

/// Schema bytes a keyword search returns. Every search result stays in the
/// transcript for the rest of the turn, so one oversized schema (some write
/// tools carry 20 KB+) is re-read at every later step. Matches past the budget
/// are listed by name; `select:` returns them in full.
const KEYWORD_SCHEMA_BUDGET_BYTES: usize = 12_000;

const BRIEF_DESCRIPTION_CHARS: usize = 140;

pub struct ToolSearchTool {
    catalog: CatalogReader,
    cmd_tx: mpsc::Sender<McpCmd>,
    spec: ToolSpec,
    sent: Mutex<SentThisTurn>,
    transcript_epoch: TranscriptEpoch,
}

/// What earlier searches in the current turn already put in the transcript,
/// so later searches in the same turn do not repeat it. Mid-turn compaction
/// can summarise those results away, so the record also resets when the
/// transcript epoch moves.
#[derive(Default)]
struct SentThisTurn {
    turn_id: Option<String>,
    epoch: u64,
    catalog: bool,
    schemas: BTreeSet<String>,
}

impl SentThisTurn {
    fn for_turn(&mut self, turn_id: &str, epoch: u64) -> &mut Self {
        if self.turn_id.as_deref() != Some(turn_id) || self.epoch != epoch {
            *self = Self {
                turn_id: Some(turn_id.to_string()),
                epoch,
                ..Self::default()
            };
        }
        self
    }
}

impl ToolSearchTool {
    pub fn new(
        catalog: CatalogReader,
        cmd_tx: mpsc::Sender<McpCmd>,
        transcript_epoch: TranscriptEpoch,
    ) -> Self {
        Self {
            catalog,
            cmd_tx,
            spec: build_spec(),
            sent: Mutex::new(SentThisTurn::default()),
            transcript_epoch,
        }
    }
}

#[derive(Debug, Deserialize)]
struct ToolSearchInput {
    query: String,
}

fn build_spec() -> ToolSpec {
    let input_schema = json!({
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "Keywords to match against tool names and descriptions, \
                    or `select:` followed by comma-separated exact tool names.",
            },
        },
        "required": ["query"],
        "additionalProperties": false,
    });

    let description = "Search the catalog of MCP-backed tools attached to this assistant. \
Those tools are not listed in your declared tool schema; this is how you discover them. \
Results carry full input schemas for matches, a compact name index of the whole catalog \
(sent once per turn; later searches refer back to it), and the connection status of every \
attached MCP server (including authorization links for servers that require auth). \
Schemas already returned this turn are not repeated. Search once with all the keywords you \
need rather than several narrow searches; use `select:` for exact names you already know. A discovered tool is invoked by its exact name — \
directly, or from a compose script via tool(name, input); it never appears in your \
declared tool list. Also callable from inside compose scripts.";

    ToolSpec::new(TOOL_NAME, description, input_schema)
        .with_annotations(agentkit_tools_core::ToolAnnotations::default().with_idempotent(true))
}

#[async_trait]
impl Tool for ToolSearchTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        _ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        let input: ToolSearchInput = serde_json::from_value(request.input)
            .map_err(|e| ToolError::InvalidInput(e.to_string()))?;

        let (reply_tx, reply_rx) = oneshot::channel();
        self.cmd_tx
            .send(McpCmd::EnsureConnected {
                reply: reply_tx,
                parent: tracing::Span::current(),
            })
            .await
            .map_err(|_| ToolError::Unavailable("mcp actor unavailable".into()))?;
        let servers: Vec<McpServerStatus> = reply_rx
            .await
            .map_err(|_| ToolError::Unavailable("mcp actor dropped reply".into()))?;

        // Restrict search to tools owned by a currently configured server.
        // agentkit's disconnect can fail to unregister a detached server's
        // tools from the catalog (the handle is dropped before close() can
        // fail), so the raw catalog may briefly contain tools that are no
        // longer callable; the status list is the authoritative view.
        let live_names: std::collections::BTreeSet<&str> = servers
            .iter()
            .flat_map(|server| server.tools.iter().map(String::as_str))
            .collect();
        let specs: Vec<ToolSpec> = self
            .catalog
            .specs()
            .into_iter()
            .filter(|spec| live_names.contains(spec.name.0.as_str()))
            .collect();
        let selecting = input.query.trim().starts_with("select:");
        let server_ids: Vec<&str> = servers.iter().map(|server| server.id.as_str()).collect();
        let matches = rank(&specs, &input.query, &server_ids);

        let epoch = self.transcript_epoch.load(Ordering::Acquire);
        let mut sent = self.sent.lock().unwrap_or_else(PoisonError::into_inner);
        let sent = sent.for_turn(&request.turn_id.0, epoch);

        let mut matched_tools: Vec<Value> = Vec::new();
        let mut already_returned: Vec<String> = Vec::new();
        let mut over_budget: Vec<Value> = Vec::new();
        let mut schema_bytes = 0usize;
        for (rank_index, spec) in matches.into_iter().enumerate() {
            let name = spec.name.0.clone();
            if !selecting && sent.schemas.contains(&name) {
                already_returned.push(name);
                continue;
            }
            let entry = match_entry(spec);
            let size = entry.to_string().len();
            // The best match is always returned, so a search never comes back
            // empty-handed just because its top hit is large.
            if !selecting && rank_index > 0 && schema_bytes + size > KEYWORD_SCHEMA_BUDGET_BYTES {
                over_budget.push(json!({ "name": name, "brief": brief(&spec.description) }));
                continue;
            }
            schema_bytes += size;
            sent.schemas.insert(name);
            matched_tools.push(entry);
        }

        let mut body = serde_json::Map::new();
        body.insert("matched_tools".into(), Value::Array(matched_tools));
        if !already_returned.is_empty() {
            body.insert(
                "already_returned".into(),
                json!({
                    "note": "Schemas returned by an earlier tool_search this turn.",
                    "names": already_returned,
                }),
            );
        }
        if !over_budget.is_empty() {
            body.insert(
                "more_matches".into(),
                json!({
                    "note": "Also matched; get a schema with select:<name>.",
                    "tools": over_budget,
                }),
            );
        }
        if sent.catalog {
            body.insert(
                "catalog".into(),
                Value::String("Listed in an earlier tool_search result this turn.".into()),
            );
        } else {
            sent.catalog = true;
            let catalog_index: Vec<Value> = specs
                .iter()
                .map(|spec| json!({ "name": spec.name.0, "brief": brief(&spec.description) }))
                .collect();
            body.insert("catalog".into(), Value::Array(catalog_index));
        }
        // Tool names are already in the catalog; servers carry only status.
        let server_statuses: Vec<Value> = servers.iter().map(server_status).collect();
        body.insert("servers".into(), Value::Array(server_statuses));
        let body = Value::Object(body);

        Ok(ToolResult::new(ToolResultPart::success(
            request.call_id,
            ToolOutput::Structured(body),
        )))
    }
}

fn match_entry(spec: &ToolSpec) -> Value {
    let mut entry = serde_json::Map::new();
    entry.insert("name".into(), Value::String(spec.name.0.clone()));
    entry.insert(
        "description".into(),
        Value::String(spec.description.clone()),
    );
    entry.insert("input_schema".into(), spec.input_schema.clone());
    if let Some(output_schema) = &spec.output_schema {
        entry.insert("output_schema".into(), output_schema.clone());
    }
    Value::Object(entry)
}

fn server_status(server: &McpServerStatus) -> Value {
    let mut status = json!({ "id": server.id, "status": server.status });
    if let Some(url) = &server.authorization_url {
        status["authorization_url"] = Value::String(url.clone());
    }
    if let Some(error) = &server.error {
        status["error"] = Value::String(error.clone());
    }
    status
}

/// Whether `wanted` names `full_name`, either exactly or as the whole tool
/// name after one attached server's `mcp_<server id>_` namespace.
fn names_tool(full_name: &str, wanted: &str, server_ids: &[&str]) -> bool {
    full_name == wanted
        || server_ids.iter().any(|id| {
            full_name
                .strip_prefix("mcp_")
                .and_then(|rest| rest.strip_prefix(id))
                .and_then(|rest| rest.strip_prefix('_'))
                == Some(wanted)
        })
}

fn brief(description: &str) -> String {
    let first_line = description.lines().next().unwrap_or_default();
    let mut out: String = first_line.chars().take(BRIEF_DESCRIPTION_CHARS).collect();
    if first_line.chars().count() > BRIEF_DESCRIPTION_CHARS {
        out.push('…');
    }
    out
}

fn rank<'a>(specs: &'a [ToolSpec], query: &str, server_ids: &[&str]) -> Vec<&'a ToolSpec> {
    let query = query.trim();
    if let Some(selection) = query.strip_prefix("select:") {
        let wanted: Vec<&str> = selection
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .collect();
        // A name may be given without its MCP server prefix
        // (`get_project_overview` for `mcp__p-platform_get_project_overview`),
        // the way instructions and server docs name tools.
        return specs
            .iter()
            .filter(|spec| {
                wanted
                    .iter()
                    .any(|w| names_tool(&spec.name.0, w, server_ids))
            })
            .collect();
    }

    let tokens: Vec<String> = query
        .split(|c: char| !c.is_alphanumeric())
        .filter(|t| !t.is_empty())
        .map(str::to_lowercase)
        .collect();
    if tokens.is_empty() {
        return Vec::new();
    }

    let mut scored: Vec<(usize, &ToolSpec)> = specs
        .iter()
        .filter_map(|spec| {
            let name = spec.name.0.to_lowercase();
            let description = spec.description.to_lowercase();
            let score: usize = tokens
                .iter()
                .map(|t| {
                    if name.contains(t.as_str()) {
                        2
                    } else if description.contains(t.as_str()) {
                        1
                    } else {
                        0
                    }
                })
                .sum();
            (score > 0).then_some((score, spec))
        })
        .collect();
    scored.sort_by(|a, b| b.0.cmp(&a.0).then_with(|| a.1.name.0.cmp(&b.1.name.0)));
    scored
        .into_iter()
        .take(MAX_MATCHES)
        .map(|(_, spec)| spec)
        .collect()
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;

    fn spec(name: &str, description: &str) -> ToolSpec {
        ToolSpec::new(
            name,
            description,
            json!({"type": "object", "properties": {}}),
        )
    }

    #[test]
    fn rank_prefers_name_hits_over_description_hits() {
        let specs = vec![
            spec("mcp_srv_get_weather", "Reads a forecast."),
            spec("mcp_srv_list_cities", "Cities with weather stations."),
            spec("mcp_srv_unrelated", "Nothing relevant."),
        ];
        let ranked = rank(&specs, "weather", &["srv"]);
        let names: Vec<&str> = ranked.iter().map(|s| s.name.0.as_str()).collect();
        assert_eq!(names, ["mcp_srv_get_weather", "mcp_srv_list_cities"]);
    }

    #[test]
    fn rank_select_matches_exact_names_only() {
        let specs = vec![
            spec("mcp_srv_get_weather", "Reads a forecast."),
            spec("mcp_srv_get_weather_history", "Historical data."),
        ];
        let ranked = rank(&specs, "select: mcp_srv_get_weather", &["srv"]);
        let names: Vec<&str> = ranked.iter().map(|s| s.name.0.as_str()).collect();
        assert_eq!(names, ["mcp_srv_get_weather"]);
    }

    #[test]
    fn rank_select_accepts_names_without_the_server_prefix() {
        let specs = vec![
            spec("mcp__p-platform_get_project_overview", "Project health."),
            spec("mcp__p-platform_get_project_overview_extra", "Other."),
            spec("get_project_overview_local", "Not namespaced."),
        ];
        let server_ids = ["_p-platform"];
        let ranked = rank(&specs, "select:get_project_overview", &server_ids);
        let names: Vec<&str> = ranked.iter().map(|s| s.name.0.as_str()).collect();
        assert_eq!(names, ["mcp__p-platform_get_project_overview"]);

        // A trailing fragment of a tool name is not a name.
        assert!(rank(&specs, "select:overview", &server_ids).is_empty());
        assert!(rank(&specs, "select:project_overview", &server_ids).is_empty());
    }

    #[test]
    fn rank_caps_full_schema_matches() {
        let specs: Vec<ToolSpec> = (0..20)
            .map(|i| spec(&format!("mcp_srv_widget_{i}"), "Manages widgets."))
            .collect();
        assert_eq!(rank(&specs, "widget", &["srv"]).len(), MAX_MATCHES);
    }

    #[test]
    fn rank_empty_query_matches_nothing() {
        let specs = vec![spec("mcp_srv_get_weather", "Reads a forecast.")];
        assert!(rank(&specs, "   ", &["srv"]).is_empty());
    }

    #[test]
    fn brief_takes_first_line_and_caps_length() {
        let long = format!("{}\nsecond line", "x".repeat(200));
        let b = brief(&long);
        assert!(b.chars().count() == BRIEF_DESCRIPTION_CHARS + 1);
        assert!(b.ends_with('…'));
        assert_eq!(brief("short one."), "short one.");
    }

    struct StubTool {
        spec: ToolSpec,
    }

    #[async_trait]
    impl Tool for StubTool {
        fn spec(&self) -> &ToolSpec {
            &self.spec
        }

        async fn invoke(
            &self,
            request: ToolRequest,
            _ctx: &mut ToolContext<'_>,
        ) -> Result<ToolResult, ToolError> {
            Ok(ToolResult::new(ToolResultPart::success(
                request.call_id,
                ToolOutput::text("ok"),
            )))
        }
    }

    struct AllowAll;

    impl agentkit_tools_core::PermissionChecker for AllowAll {
        fn evaluate(
            &self,
            _request: &dyn agentkit_tools_core::PermissionRequest,
        ) -> agentkit_tools_core::PermissionDecision {
            agentkit_tools_core::PermissionDecision::Allow
        }
    }

    fn stub(name: &str, schema_padding: usize) -> std::sync::Arc<dyn Tool> {
        std::sync::Arc::new(StubTool {
            spec: ToolSpec::new(
                name,
                "Manages widgets.",
                json!({
                    "type": "object",
                    "properties": { "note": { "type": "string", "description": "x".repeat(schema_padding) } },
                }),
            ),
        })
    }

    async fn search(tool: &ToolSearchTool, turn: &str, query: &str) -> Value {
        let owned = agentkit_tools_core::OwnedToolContext {
            session_id: "s".into(),
            turn_id: turn.into(),
            metadata: Default::default(),
            permissions: std::sync::Arc::new(AllowAll),
            resources: std::sync::Arc::new(()),
            cancellation: None,
            execution_scope: None,
            approved_request: None,
        };
        let request = ToolRequest::new("call", TOOL_NAME, json!({ "query": query }), "s", turn);
        let result = tool.invoke(request, &mut owned.borrowed()).await.unwrap();
        match result.result.output {
            ToolOutput::Structured(value) => value,
            other => panic!("unexpected output: {other:?}"),
        }
    }

    fn names(value: &Value) -> Vec<String> {
        value
            .as_array()
            .unwrap()
            .iter()
            .map(|entry| entry["name"].as_str().unwrap().to_string())
            .collect()
    }

    #[tokio::test]
    async fn searches_send_the_catalog_and_each_schema_once_per_turn() {
        let (writer, reader) = agentkit_tools_core::dynamic_catalog("mcp");
        let tool_names = ["mcp_srv_a_widget", "mcp_srv_b_widget", "mcp_srv_c_widget"];
        writer.upsert(stub(tool_names[0], 10));
        writer.upsert(stub(tool_names[1], 10));
        writer.upsert(stub(tool_names[2], KEYWORD_SCHEMA_BUDGET_BYTES));
        let (cmd_tx, mut cmd_rx) = mpsc::channel(8);
        tokio::spawn(async move {
            while let Some(cmd) = cmd_rx.recv().await {
                if let McpCmd::EnsureConnected { reply, .. } = cmd {
                    let _ = reply.send(vec![McpServerStatus {
                        id: "srv".into(),
                        status: crate::mcp_actor::McpServerConnectionStatus::Connected,
                        tools: tool_names.iter().map(|n| n.to_string()).collect(),
                        authorization_url: None,
                        error: None,
                    }]);
                }
            }
        });
        let epoch = TranscriptEpoch::default();
        let tool = ToolSearchTool::new(reader, cmd_tx, epoch.clone());

        // First search: catalog, the schemas that fit the budget, and the
        // oversized match listed by name only. Servers carry no tool lists.
        let first = search(&tool, "t1", "widget").await;
        assert_eq!(
            names(&first["matched_tools"]),
            ["mcp_srv_a_widget", "mcp_srv_b_widget"]
        );
        assert_eq!(names(&first["more_matches"]["tools"]), ["mcp_srv_c_widget"]);
        assert_eq!(first["catalog"].as_array().unwrap().len(), 3);
        assert!(first["servers"][0].get("tools").is_none());

        // Same turn: nothing repeated.
        let second = search(&tool, "t1", "widget").await;
        assert!(second["matched_tools"].as_array().unwrap().is_empty());
        assert_eq!(
            second["already_returned"]["names"],
            json!(["mcp_srv_a_widget", "mcp_srv_b_widget"])
        );
        assert!(second["catalog"].is_string());

        // select: returns exactly what was asked for, budget or not.
        let selected = search(&tool, "t1", "select:mcp_srv_c_widget").await;
        assert_eq!(names(&selected["matched_tools"]), ["mcp_srv_c_widget"]);

        // Compaction mid-turn may drop earlier results, so it starts over too.
        epoch.fetch_add(1, Ordering::AcqRel);
        let after_compaction = search(&tool, "t1", "widget").await;
        assert_eq!(
            names(&after_compaction["matched_tools"]),
            ["mcp_srv_a_widget", "mcp_srv_b_widget"]
        );
        assert_eq!(after_compaction["catalog"].as_array().unwrap().len(), 3);

        // A new turn starts over.
        let next_turn = search(&tool, "t2", "widget").await;
        assert_eq!(
            names(&next_turn["matched_tools"]),
            ["mcp_srv_a_widget", "mcp_srv_b_widget"]
        );
        assert_eq!(next_turn["catalog"].as_array().unwrap().len(), 3);
    }
}
