use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_mcp::McpServerId;
use agentkit_tools_core::{ToolError, ToolResult};
use agentkit_tools_derive::tool;
use schemars::JsonSchema;
use serde::Deserialize;
use tokio::sync::{mpsc, oneshot};

use crate::runtime::McpCmd;

pub struct McpForceReconnectTool {
    cmd_tx: mpsc::Sender<McpCmd>,
}

impl McpForceReconnectTool {
    pub fn new(cmd_tx: mpsc::Sender<McpCmd>) -> Self {
        Self { cmd_tx }
    }
}

#[derive(Debug, Deserialize, JsonSchema)]
pub struct McpForceReconnectInput {
    /// Server id as it appears in the tool name prefix `mcp_<server_id>_<tool>`.
    pub server_id: String,
}

#[tool(
    name = "mcp_force_reconnect",
    idempotent,
    description = "Disconnect and reconnect a registered MCP server. Use this when a tool \
from that server returns a connection-related error (timeout, transport closed, auth failure \
that the backend has since refreshed). The argument is the server id as it appears in the \
tool name prefix `mcp_<server_id>_<tool>`. Safe to retry; succeeds even if the server was \
not previously connected."
)]
impl McpForceReconnectTool {
    async fn run(&self, input: McpForceReconnectInput) -> Result<ToolResult, ToolError> {
        let (reply_tx, reply_rx) = oneshot::channel();
        let server_id = McpServerId::new(input.server_id.clone());
        self.cmd_tx
            .send(McpCmd::ForceReconnect {
                server_id,
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
                call_id: Default::default(),
                output: ToolOutput::text(text),
                is_error,
                metadata: MetadataMap::new(),
            },
            duration: None,
            metadata: MetadataMap::new(),
        })
    }
}
