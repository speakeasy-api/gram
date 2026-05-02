use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_mcp::McpServerId;
use agentkit_tools_core::{
    Tool, ToolAnnotations, ToolContext, ToolError, ToolName, ToolRequest, ToolResult, ToolSpec,
};
use async_trait::async_trait;
use serde::Deserialize;
use serde_json::json;
use tokio::sync::{mpsc, oneshot};

use crate::runtime::McpCmd;

#[derive(Clone)]
pub struct McpForceReconnectTool {
    cmd_tx: mpsc::Sender<McpCmd>,
    spec: ToolSpec,
}

impl McpForceReconnectTool {
    pub fn new(cmd_tx: mpsc::Sender<McpCmd>) -> Self {
        Self {
            cmd_tx,
            spec: ToolSpec {
                name: ToolName::new("mcp_force_reconnect"),
                description:
                    "Disconnect and reconnect a registered MCP server. Use this when a tool from \
that server returns a connection-related error (timeout, transport closed, auth failure that the \
backend has since refreshed). The argument is the server id as it appears in the tool name prefix \
`mcp_<server_id>_<tool>`. Safe to retry; succeeds even if the server was not previously connected."
                        .into(),
                input_schema: json!({
                    "type": "object",
                    "properties": {
                        "server_id": { "type": "string" }
                    },
                    "required": ["server_id"],
                    "additionalProperties": false
                }),
                annotations: ToolAnnotations::default()
                    .with_idempotent(true)
                    .with_destructive(false),
                metadata: MetadataMap::new(),
            },
        }
    }
}

#[derive(Deserialize)]
struct Input {
    server_id: String,
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
        let input: Input = serde_json::from_value(request.input.clone()).map_err(|e| {
            ToolError::InvalidInput(format!("invalid mcp_force_reconnect input: {e}"))
        })?;

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
            Ok(()) => (
                format!("reconnected mcp server {}", input.server_id),
                false,
            ),
            Err(e) => (
                format!("failed to reconnect mcp server {}: {e}", input.server_id),
                true,
            ),
        };

        Ok(ToolResult {
            result: ToolResultPart {
                call_id: request.call_id,
                output: ToolOutput::text(text),
                is_error,
                metadata: MetadataMap::new(),
            },
            duration: None,
            metadata: MetadataMap::new(),
        })
    }
}
