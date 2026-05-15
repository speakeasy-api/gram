use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_mcp::McpServerId;
use agentkit_tools_core::{
    Tool, ToolAnnotations, ToolContext, ToolError, ToolRequest, ToolResult, ToolSpec,
};
use async_trait::async_trait;
use serde::Deserialize;
use serde_json::{Value, json};
use tokio::sync::{mpsc, oneshot};

use crate::http_layer::{THREAD_TOKEN, TokenRegistry};
use crate::runtime::McpCmd;

const TOOL_NAME: &str = "mcp_force_reconnect";

pub struct McpForceReconnectTool {
    cmd_tx: mpsc::Sender<McpCmd>,
    spec: ToolSpec,
}

impl McpForceReconnectTool {
    pub fn new(cmd_tx: mpsc::Sender<McpCmd>, server_ids: Vec<String>) -> Self {
        let spec = build_spec(&server_ids);
        Self { cmd_tx, spec }
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

        let (reply_tx, reply_rx) = oneshot::channel();
        let server_id = McpServerId::new(input.server_id.clone());
        // Tool runs inside the calling thread's task; lift its bearer slot
        // into the McpCmd so the actor task can re-scope THREAD_TOKEN around
        // the reconnect handshake.
        let tokens = THREAD_TOKEN
            .try_with(|r| r.clone())
            .unwrap_or_else(|_| TokenRegistry::new(""));
        self.cmd_tx
            .send(McpCmd::ForceReconnect {
                server_id,
                tokens,
                reply: reply_tx,
            })
            .await
            .map_err(|_| ToolError::ExecutionFailed("mcp actor channel closed".into()))?;

        let outcome = reply_rx
            .await
            .map_err(|_| ToolError::ExecutionFailed("mcp actor dropped reply".into()))?;

        let (text, is_error) = match outcome {
            Ok(()) => (format!("reconnected mcp server {}", input.server_id), false),
            Err(e) => (
                format!("failed to reconnect mcp server {}: {e}", input.server_id),
                true,
            ),
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
