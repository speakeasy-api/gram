use std::sync::Arc;

use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_mcp::McpServerId;
use agentkit_tools_core::{
    Tool, ToolAnnotations, ToolContext, ToolError, ToolRequest, ToolResult, ToolSpec,
};
use async_trait::async_trait;
use futures::future::join_all;
use serde::Deserialize;
use serde_json::{Value, json};
use tokio::sync::oneshot;

use crate::runtime::{McpCmd, RuntimeHost};

const TOOL_NAME: &str = "mcp_force_reconnect";

pub struct McpForceReconnectTool {
    host: Arc<RuntimeHost>,
    spec: ToolSpec,
}

impl McpForceReconnectTool {
    pub fn new(host: Arc<RuntimeHost>, server_ids: Vec<String>) -> Self {
        let spec = build_spec(&server_ids);
        Self { host, spec }
    }
}

#[derive(Debug, Deserialize)]
struct McpForceReconnectInput {
    server_id: String,
}

fn build_spec(server_ids: &[String]) -> ToolSpec {
    // The model only sees tools advertised in /tools/list. Without the enum,
    // the assistant has to guess server_id from the `mcp_<id>_<tool>` prefix
    // — which fails the moment the server is disconnected (no prefixed tools
    // exposed). Baking the configured server IDs into the schema keeps the
    // tool callable even when MCP is fully offline.
    let server_ids_json: Vec<Value> = server_ids.iter().cloned().map(Value::String).collect();

    let server_id_property = if server_ids_json.is_empty() {
        json!({
            "type": "string",
            "description": "ID of the MCP server to reconnect.",
        })
    } else {
        json!({
            "type": "string",
            "description": "ID of the MCP server to reconnect.",
            "enum": server_ids_json,
        })
    };

    let input_schema = json!({
        "type": "object",
        "properties": { "server_id": server_id_property },
        "required": ["server_id"],
        "additionalProperties": false,
    });

    let description = if server_ids.is_empty() {
        "Disconnect and reconnect a registered MCP server. No MCP servers are \
configured for this assistant; calling this tool will fail."
    } else {
        "Disconnect and reconnect a registered MCP server. Use this when an \
MCP-backed tool returns a connection-related error (timeout, transport closed, \
auth failure that the backend has since refreshed) or when no MCP-backed tools \
appear in the catalog."
    };

    ToolSpec::new(TOOL_NAME, description, input_schema)
        .with_annotations(ToolAnnotations::default().with_idempotent(true))
}

#[async_trait]
impl Tool for McpForceReconnectTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        _ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        let call_id = request.call_id.clone();
        let input: McpForceReconnectInput = serde_json::from_value(request.input)
            .map_err(|e| ToolError::InvalidInput(e.to_string()))?;

        // The model only sees one tool; fan out to every live thread's
        // actor so sibling threads' transports get reseated too.
        let senders: Vec<_> = self
            .host
            .threads
            .iter()
            .filter_map(|entry| entry.value().get().map(|t| t.mcp_cmd_tx.clone()))
            .collect();

        let total = senders.len();
        let (text, is_error) = if total == 0 {
            (
                format!("no live threads to reconnect mcp server {}", input.server_id),
                true,
            )
        } else {
            let outcomes = join_all(senders.into_iter().map(|tx| {
                let server_id = McpServerId::new(input.server_id.clone());
                async move {
                    let (reply_tx, reply_rx) = oneshot::channel();
                    if tx
                        .send(McpCmd::ForceReconnect {
                            server_id,
                            reply: reply_tx,
                        })
                        .await
                        .is_err()
                    {
                        return Err("mcp actor channel closed".to_string());
                    }
                    reply_rx
                        .await
                        .map_err(|_| "mcp actor dropped reply".to_string())?
                }
            }))
            .await;

            let (ok, errs): (usize, Vec<String>) = outcomes.into_iter().fold(
                (0, Vec::new()),
                |(ok, mut errs), outcome| match outcome {
                    Ok(()) => (ok + 1, errs),
                    Err(e) => {
                        errs.push(e);
                        (ok, errs)
                    }
                },
            );

            let mut text = format!(
                "reconnected mcp server {} on {ok}/{total} threads",
                input.server_id
            );
            if !errs.is_empty() {
                text.push_str(&format!("; errors: {}", errs.join(", ")));
            }
            (text, ok == 0)
        };

        Ok(ToolResult {
            result: ToolResultPart {
                call_id,
                output: ToolOutput::text(text),
                is_error,
                metadata: MetadataMap::new(),
            },
            duration: None,
            metadata: MetadataMap::new(),
        })
    }
}
