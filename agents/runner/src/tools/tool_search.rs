use agentkit_core::{ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    CatalogReader, Tool, ToolContext, ToolError, ToolRequest, ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde::Deserialize;
use serde_json::{Value, json};
use tokio::sync::{mpsc, oneshot};

use crate::mcp_actor::{McpCmd, McpServerConnectionStatus, McpServerStatus};

pub const TOOL_NAME: &str = "tool_search";

/// Full schemas returned per search. The compact name index always covers
/// the whole catalog, so a miss costs one more search, not a blind spot.
const MAX_MATCHES: usize = 8;

const BRIEF_DESCRIPTION_CHARS: usize = 140;

pub struct ToolSearchTool {
    catalog: CatalogReader,
    cmd_tx: mpsc::Sender<McpCmd>,
    spec: ToolSpec,
}

impl ToolSearchTool {
    pub fn new(catalog: CatalogReader, cmd_tx: mpsc::Sender<McpCmd>) -> Self {
        Self {
            catalog,
            cmd_tx,
            spec: build_spec(),
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
Keyword results carry full input schemas for matches, a compact name index of the whole \
catalog, and the connection status of every attached MCP server (including authorization \
links for servers that require auth). A `select:` query returns only the named tools' \
schemas, plus any names it could not find and any server that is not connected. \
A discovered tool is invoked by its exact name — directly, or from a compose script via \
tool(name, input); it never appears in your declared tool list. Also callable from \
inside compose scripts.";

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
        let body = search_body(&specs, &input.query, &servers);

        Ok(ToolResult::new(ToolResultPart::success(
            request.call_id,
            ToolOutput::Structured(body),
        )))
    }
}

/// Builds the search result. A `select:` query names the tools it wants, so it
/// gets back only their schemas: the catalog index and the per-server tool
/// lists would repeat what the model already has, on every select. Names that
/// matched nothing and servers that are not connected (which carry the
/// authorization link) are still reported, so a miss stays explainable.
fn search_body(specs: &[ToolSpec], query: &str, servers: &[McpServerStatus]) -> Value {
    let matches = rank(specs, query);
    let matched_tools: Vec<Value> = matches.iter().map(|spec| full_entry(spec)).collect();

    if let Some(wanted) = selection(query).filter(|wanted| !wanted.is_empty()) {
        let mut body = serde_json::Map::new();
        body.insert("matched_tools".into(), Value::Array(matched_tools));
        let not_found: Vec<&str> = wanted
            .into_iter()
            .filter(|w| !matches.iter().any(|spec| spec.name.0 == *w))
            .collect();
        if !not_found.is_empty() {
            body.insert("not_found".into(), json!(not_found));
        }
        let unconnected: Vec<&McpServerStatus> = servers
            .iter()
            .filter(|server| server.status != McpServerConnectionStatus::Connected)
            .collect();
        if !unconnected.is_empty() {
            body.insert("servers".into(), json!(unconnected));
        }
        return Value::Object(body);
    }

    let catalog_index: Vec<Value> = specs
        .iter()
        .map(|spec| {
            json!({
                "name": spec.name.0,
                "brief": brief(&spec.description),
            })
        })
        .collect();

    json!({
        "matched_tools": matched_tools,
        "catalog": catalog_index,
        "servers": servers,
    })
}

fn full_entry(spec: &ToolSpec) -> Value {
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

/// The exact names a `select:` query asks for, or `None` for a keyword query.
fn selection(query: &str) -> Option<Vec<&str>> {
    query.trim().strip_prefix("select:").map(|selection| {
        selection
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .collect()
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

fn rank<'a>(specs: &'a [ToolSpec], query: &str) -> Vec<&'a ToolSpec> {
    if let Some(wanted) = selection(query) {
        return specs
            .iter()
            .filter(|spec| wanted.iter().any(|w| spec.name.0 == *w))
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
#[allow(clippy::unwrap_used, clippy::expect_used)]
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
        let ranked = rank(&specs, "weather");
        let names: Vec<&str> = ranked.iter().map(|s| s.name.0.as_str()).collect();
        assert_eq!(names, ["mcp_srv_get_weather", "mcp_srv_list_cities"]);
    }

    #[test]
    fn rank_select_matches_exact_names_only() {
        let specs = vec![
            spec("mcp_srv_get_weather", "Reads a forecast."),
            spec("mcp_srv_get_weather_history", "Historical data."),
        ];
        let ranked = rank(&specs, "select: mcp_srv_get_weather");
        let names: Vec<&str> = ranked.iter().map(|s| s.name.0.as_str()).collect();
        assert_eq!(names, ["mcp_srv_get_weather"]);
    }

    #[test]
    fn rank_caps_full_schema_matches() {
        let specs: Vec<ToolSpec> = (0..20)
            .map(|i| spec(&format!("mcp_srv_widget_{i}"), "Manages widgets."))
            .collect();
        assert_eq!(rank(&specs, "widget").len(), MAX_MATCHES);
    }

    #[test]
    fn rank_empty_query_matches_nothing() {
        let specs = vec![spec("mcp_srv_get_weather", "Reads a forecast.")];
        assert!(rank(&specs, "   ").is_empty());
    }

    fn status(id: &str, status: McpServerConnectionStatus, tools: &[&str]) -> McpServerStatus {
        McpServerStatus {
            id: id.into(),
            status,
            tools: tools.iter().map(|t| (*t).to_string()).collect(),
            authorization_url: None,
            error: None,
        }
    }

    #[test]
    fn select_returns_only_the_selected_schemas() {
        let specs = vec![
            spec("mcp_srv_get_weather", "Reads a forecast."),
            spec("mcp_srv_list_cities", "Cities with weather stations."),
        ];
        let servers = vec![
            status(
                "srv",
                McpServerConnectionStatus::Connected,
                &["mcp_srv_get_weather", "mcp_srv_list_cities"],
            ),
            status(
                "auth",
                McpServerConnectionStatus::AuthorizationRequired,
                &[],
            ),
        ];
        let body = search_body(&specs, "select:mcp_srv_get_weather,mcp_srv_nope", &servers);
        let keys: Vec<&str> = body
            .as_object()
            .unwrap()
            .keys()
            .map(String::as_str)
            .collect();
        assert_eq!(keys, ["matched_tools", "not_found", "servers"]);
        assert_eq!(body["matched_tools"].as_array().unwrap().len(), 1);
        assert_eq!(body["matched_tools"][0]["name"], "mcp_srv_get_weather");
        assert_eq!(body["not_found"], json!(["mcp_srv_nope"]));
        assert_eq!(
            body["servers"],
            json!([{"id": "auth", "status": "authorization_required"}])
        );

        let body = search_body(&specs, "select:mcp_srv_get_weather", &servers[..1]);
        let keys: Vec<&str> = body
            .as_object()
            .unwrap()
            .keys()
            .map(String::as_str)
            .collect();
        assert_eq!(
            keys,
            ["matched_tools"],
            "a clean select carries nothing else"
        );
    }

    #[test]
    fn keyword_search_keeps_catalog_index_and_server_status() {
        let specs = vec![spec("mcp_srv_get_weather", "Reads a forecast.")];
        let servers = vec![status(
            "srv",
            McpServerConnectionStatus::Connected,
            &["mcp_srv_get_weather"],
        )];
        let body = search_body(&specs, "weather", &servers);
        assert_eq!(body["matched_tools"][0]["name"], "mcp_srv_get_weather");
        assert_eq!(body["catalog"][0]["name"], "mcp_srv_get_weather");
        assert_eq!(body["servers"][0]["id"], "srv");
    }

    #[test]
    fn bare_select_falls_back_to_the_catalog_index() {
        let specs = vec![spec("mcp_srv_get_weather", "Reads a forecast.")];
        let servers = vec![status(
            "srv",
            McpServerConnectionStatus::Connected,
            &["mcp_srv_get_weather"],
        )];
        let body = search_body(&specs, "select:", &servers);
        assert_eq!(body["catalog"][0]["name"], "mcp_srv_get_weather");
        assert_eq!(body["servers"][0]["id"], "srv");
    }

    #[test]
    fn brief_takes_first_line_and_caps_length() {
        let long = format!("{}\nsecond line", "x".repeat(200));
        let b = brief(&long);
        assert!(b.chars().count() == BRIEF_DESCRIPTION_CHARS + 1);
        assert!(b.ends_with('…'));
        assert_eq!(brief("short one."), "short one.");
    }
}
