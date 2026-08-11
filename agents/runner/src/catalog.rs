//! Frozen-schema exposure of the MCP catalog.
//!
//! Tool definitions serialize ahead of the message history in every
//! provider's prompt-cache prefix, so any change to the declared tool set
//! invalidates the cache for the entire transcript. The runner therefore
//! declares a fixed tool set for the lifetime of a thread and never
//! advertises MCP tools directly: [`HiddenCatalogSource`] exposes the MCP
//! catalog for name-based dispatch (direct calls and compose scripts)
//! while advertising nothing, and `tool_search` delivers schemas in-band
//! as tool results. Providers accept histories containing calls to
//! undeclared names; models that grammar-constrain function names to the
//! declared set route through `compose` instead.

use std::sync::Arc;

use agentkit_core::{ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    CatalogReader, PermissionRequest, Tool, ToolCatalogEvent, ToolContext, ToolError, ToolName,
    ToolRequest, ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde_json::json;
use tokio::sync::{mpsc, oneshot};

use crate::mcp_actor::McpCmd;

/// Exposes the MCP catalog for dispatch without advertising any specs.
pub struct HiddenCatalogSource {
    catalog: CatalogReader,
    cmd_tx: mpsc::Sender<McpCmd>,
}

impl HiddenCatalogSource {
    pub fn new(catalog: CatalogReader, cmd_tx: mpsc::Sender<McpCmd>) -> Self {
        Self { catalog, cmd_tx }
    }
}

impl ToolSource for HiddenCatalogSource {
    fn specs(&self) -> Vec<ToolSpec> {
        Vec::new()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        let inner = self.catalog.get(name)?;
        Some(Arc::new(ReconnectingTool {
            inner,
            cmd_tx: self.cmd_tx.clone(),
        }))
    }

    fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
        // Drain the underlying broadcast receiver but surface nothing:
        // the declared tool set is frozen, and catalog changes reach the
        // model through the disclosure notices and tool_search instead.
        let _ = self.catalog.drain_catalog_events();
        Vec::new()
    }
}

/// Wraps an MCP-backed tool so a transport-shaped failure reseats the
/// server connection before the error returns to the model. The failed
/// call is never replayed automatically — MCP tools are not guaranteed
/// idempotent and a mid-flight transport error is ambiguous about whether
/// the server acted — so the model decides whether to retry against the
/// fresh connection.
struct ReconnectingTool {
    inner: Arc<dyn Tool>,
    cmd_tx: mpsc::Sender<McpCmd>,
}

impl ReconnectingTool {
    async fn request_reconnect(&self) -> Result<(), String> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.cmd_tx
            .send(McpCmd::ReconnectTool {
                tool_name: self.inner.spec().name.0.clone(),
                reply: reply_tx,
            })
            .await
            .map_err(|_| "mcp actor unavailable".to_string())?;
        reply_rx
            .await
            .map_err(|_| "mcp actor dropped reconnect reply".to_string())?
    }
}

fn transport_suspect(err: &ToolError) -> bool {
    matches!(
        err,
        ToolError::ExecutionFailed(_) | ToolError::Unavailable(_) | ToolError::Internal(_)
    )
}

#[async_trait]
impl Tool for ReconnectingTool {
    fn spec(&self) -> &ToolSpec {
        self.inner.spec()
    }

    fn current_spec(&self) -> Option<ToolSpec> {
        self.inner.current_spec()
    }

    fn proposed_requests(
        &self,
        request: &ToolRequest,
    ) -> Result<Vec<Box<dyn PermissionRequest>>, ToolError> {
        self.inner.proposed_requests(request)
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        match self.inner.invoke(request, ctx).await {
            Err(err) if transport_suspect(&err) => {
                let note = match self.request_reconnect().await {
                    Ok(()) => "the MCP connection was reset; retry the call".to_string(),
                    Err(reason) => reason,
                };
                Err(ToolError::ExecutionFailed(format!("{err}; {note}")))
            }
            other => other,
        }
    }
}

/// Terminal fallback source: resolves every name to a tool that returns an
/// instructive error. Mounted last so real tools always win; its purpose is
/// to turn calls to hallucinated or undiscovered names into a recovery path
/// instead of a bare "tool not found".
pub struct UnknownToolSource;

impl ToolSource for UnknownToolSource {
    fn specs(&self) -> Vec<ToolSpec> {
        Vec::new()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        Some(Arc::new(UnknownTool {
            spec: ToolSpec::new(
                name.clone(),
                "Unknown tool placeholder.",
                json!({"type": "object", "additionalProperties": true}),
            ),
        }))
    }
}

struct UnknownTool {
    spec: ToolSpec,
}

#[async_trait]
impl Tool for UnknownTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    fn current_spec(&self) -> Option<ToolSpec> {
        None
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        _ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        Ok(ToolResult::new(ToolResultPart::error(
            request.call_id,
            ToolOutput::text(unknown_tool_message(&self.spec.name)),
        )))
    }
}

fn unknown_tool_message(name: &ToolName) -> String {
    format!(
        "tool '{name}' is not in the catalog. Use tool_search to discover available \
         tools and their schemas, then call a discovered tool by its exact name."
    )
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use agentkit_tools_core::dynamic_catalog;

    struct EchoTool {
        spec: ToolSpec,
    }

    #[async_trait]
    impl Tool for EchoTool {
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
                ToolOutput::text("echo"),
            )))
        }
    }

    fn echo() -> Arc<dyn Tool> {
        Arc::new(EchoTool {
            spec: ToolSpec::new(
                "mcp_srv_echo",
                "echoes",
                json!({"type": "object", "properties": {}}),
            ),
        })
    }

    #[tokio::test]
    async fn hidden_catalog_advertises_nothing_but_resolves() {
        let (writer, reader) = dynamic_catalog("mcp");
        writer.upsert(echo());
        let (cmd_tx, _cmd_rx) = mpsc::channel(1);
        let source = HiddenCatalogSource::new(reader, cmd_tx);

        assert!(source.specs().is_empty());
        assert!(source.get(&ToolName::new("mcp_srv_echo")).is_some());
        assert!(source.get(&ToolName::new("missing")).is_none());
        assert!(
            source.drain_catalog_events().is_empty(),
            "catalog churn must never surface as a spec change"
        );
    }

    #[test]
    fn unknown_tool_resolves_any_name_without_advertising() {
        let source = UnknownToolSource;
        let tool = source.get(&ToolName::new("gobblygoop")).unwrap();
        assert!(
            tool.current_spec().is_none(),
            "placeholder must never be advertised"
        );
        assert!(source.specs().is_empty());
        assert!(unknown_tool_message(&ToolName::new("gobblygoop")).contains("tool_search"));
    }
}
