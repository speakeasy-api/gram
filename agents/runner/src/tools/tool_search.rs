use agentkit_core::{ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    CatalogReader, Tool, ToolContext, ToolError, ToolRequest, ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde::Deserialize;
use serde_json::{Value, json};
use tokio::sync::{mpsc, oneshot};

use crate::mcp_actor::{McpCmd, McpServerStatus};

const TOOL_NAME: &str = "tool_search";

/// Full schemas returned per search. The compact name index always covers
/// the whole catalog, so a miss costs one more search, not a blind spot.
const MAX_MATCHES: usize = 8;

const BRIEF_DESCRIPTION_CHARS: usize = 140;

/// Query prefix marking a catalog request: the user asked what tools exist,
/// rather than the model looking for the tools a task needs. It carries intent
/// for the host — which may present such a search to the user as a browsable
/// catalog — and nothing for ranking, so it is stripped before matching.
const BROWSE_PREFIX: &str = "browse:";

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
                    `select:` followed by comma-separated exact tool names, or `browse:` \
                    (optionally followed by keywords) when the user asked what tools exist.",
            },
        },
        "required": ["query"],
        "additionalProperties": false,
    });

    let description = "Search the catalog of MCP-backed tools attached to this assistant. \
Those tools are not listed in your declared tool schema; this is how you discover them. \
Results carry full input schemas for matches, a compact name index of the whole catalog, \
and the connection status of every attached MCP server (including authorization links \
for servers that require auth). When the user is asking what tools or capabilities exist \
rather than looking for one to complete a task, prefix the query with `browse:` \
(alone, or followed by keywords to lead with one area); the host may present that search \
to the user as a browsable catalog. A discovered tool is invoked by its exact name — \
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
            .send(McpCmd::EnsureConnected { reply: reply_tx })
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
        let matches = rank(&specs, &input.query);

        let matched_tools: Vec<Value> = matches
            .iter()
            .map(|spec| {
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
            })
            .collect();

        let catalog_index: Vec<Value> = specs
            .iter()
            .map(|spec| {
                json!({
                    "name": spec.name.0,
                    "brief": brief(&spec.description),
                })
            })
            .collect();

        let body = json!({
            "matched_tools": matched_tools,
            "catalog": catalog_index,
            "servers": servers,
        });

        Ok(ToolResult::new(ToolResultPart::success(
            request.call_id,
            ToolOutput::Structured(body),
        )))
    }
}

fn brief(description: &str) -> String {
    let first_line = description.lines().next().unwrap_or_default();
    let mut out: String = first_line.chars().take(BRIEF_DESCRIPTION_CHARS).collect();
    if first_line.chars().count() > BRIEF_DESCRIPTION_CHARS {
        out.push('…');
    }
    out
}

/// Drops a leading `browse:` marker, case-insensitively, along with the
/// whitespace after it. `get` rather than a slice: the query is arbitrary text
/// and indexing it could land inside a multi-byte character.
fn strip_browse_prefix(query: &str) -> &str {
    match query.get(..BROWSE_PREFIX.len()) {
        Some(head) if head.eq_ignore_ascii_case(BROWSE_PREFIX) => {
            query[BROWSE_PREFIX.len()..].trim_start()
        }
        _ => query,
    }
}

fn rank<'a>(specs: &'a [ToolSpec], query: &str) -> Vec<&'a ToolSpec> {
    // A browse marker is for the host, not for matching. Stripping it keeps
    // `browse:` alone equivalent to an empty query — the catalog index carries
    // the whole list already, so a browse needs no full schemas — and lets
    // `browse: logs` still lead with the area the user named.
    let query = strip_browse_prefix(query.trim());
    if let Some(selection) = query.strip_prefix("select:") {
        let wanted: Vec<&str> = selection
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .collect();
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
    fn rank_ignores_the_browse_marker() {
        let specs = vec![
            spec("mcp_srv_search_logs", "Reads logs."),
            spec("mcp_srv_get_weather", "Reads a forecast."),
        ];
        assert!(rank(&specs, "browse:").is_empty());
        assert!(rank(&specs, "  BROWSE:  ").is_empty());
        let names: Vec<&str> = rank(&specs, "browse: logs")
            .iter()
            .map(|s| s.name.0.as_str())
            .collect();
        assert_eq!(names, ["mcp_srv_search_logs"]);
    }

    #[test]
    fn rank_leaves_a_non_marker_query_alone() {
        let specs = vec![spec("mcp_srv_browse_catalog", "Browses the catalog.")];
        let names: Vec<&str> = rank(&specs, "browse")
            .iter()
            .map(|s| s.name.0.as_str())
            .collect();
        assert_eq!(names, ["mcp_srv_browse_catalog"]);
    }

    #[test]
    fn rank_empty_query_matches_nothing() {
        let specs = vec![spec("mcp_srv_get_weather", "Reads a forecast.")];
        assert!(rank(&specs, "   ").is_empty());
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
