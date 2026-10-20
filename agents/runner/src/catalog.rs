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
    CatalogReader, PermissionDecision, PermissionRequest, Tool, ToolCatalogEvent, ToolContext,
    ToolError, ToolExecutionOutcome, ToolInterruption, ToolName, ToolRequest, ToolResult,
    ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde_json::json;
use tokio::sync::{mpsc, oneshot};

use crate::mcp_actor::{KnownTools, McpCmd};

/// Exposes the MCP catalog for dispatch without advertising any specs.
pub struct HiddenCatalogSource {
    catalog: Arc<CatalogReader>,
    cmd_tx: mpsc::Sender<McpCmd>,
    known: KnownTools,
}

impl HiddenCatalogSource {
    pub fn new(catalog: CatalogReader, cmd_tx: mpsc::Sender<McpCmd>, known: KnownTools) -> Self {
        Self {
            catalog: Arc::new(catalog),
            cmd_tx,
            known,
        }
    }
}

impl ToolSource for HiddenCatalogSource {
    fn specs(&self) -> Vec<ToolSpec> {
        Vec::new()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        if let Some(inner) = self.catalog.get(name) {
            return Some(Arc::new(ReconnectingTool {
                inner,
                cmd_tx: self.cmd_tx.clone(),
            }));
        }
        // A tool discovered earlier whose session has since closed: connect
        // its server when the call dispatches rather than reporting it unknown.
        self.known.contains(&name.0).then(|| {
            Arc::new(DeferredMcpTool {
                spec: placeholder_spec(name),
                catalog: self.catalog.clone(),
                cmd_tx: self.cmd_tx.clone(),
            }) as Arc<dyn Tool>
        })
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

impl ReconnectingTool {
    async fn reconnected(&self, err: ToolError) -> ToolError {
        let note = match self.request_reconnect().await {
            Ok(()) => "the MCP connection was reset; retry the call".to_string(),
            Err(reason) => reason,
        };
        ToolError::ExecutionFailed(format!("{err}; {note}"))
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
            Err(err) if transport_suspect(&err) => Err(self.reconnected(err).await),
            other => other,
        }
    }

    async fn invoke_outcome(
        &self,
        request: ToolRequest,
        ctx: &mut ToolContext<'_>,
    ) -> ToolExecutionOutcome {
        match self.inner.invoke_outcome(request, ctx).await {
            ToolExecutionOutcome::Failed(err) if transport_suspect(&err) => {
                ToolExecutionOutcome::Failed(self.reconnected(err).await)
            }
            other => other,
        }
    }
}

/// A discovered MCP tool whose server is not connected right now. Invoking it
/// asks the actor to connect the owning server, then dispatches through the
/// fresh session's real tool: its own permission requests are evaluated
/// exactly as the executor would for a direct call, before it runs.
struct DeferredMcpTool {
    spec: ToolSpec,
    catalog: Arc<CatalogReader>,
    cmd_tx: mpsc::Sender<McpCmd>,
}

impl DeferredMcpTool {
    async fn connect(&self) -> Result<(), String> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.cmd_tx
            .send(McpCmd::ConnectForTool {
                tool_name: self.spec.name.0.clone(),
                reply: reply_tx,
            })
            .await
            .map_err(|_| "mcp actor unavailable".to_string())?;
        reply_rx
            .await
            .map_err(|_| "mcp actor dropped connect reply".to_string())?
    }
}

#[async_trait]
impl Tool for DeferredMcpTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    fn current_spec(&self) -> Option<ToolSpec> {
        None
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        match self.invoke_outcome(request, ctx).await {
            ToolExecutionOutcome::Completed(result) => Ok(result),
            ToolExecutionOutcome::Failed(error) => Err(error),
            ToolExecutionOutcome::Interrupted(ToolInterruption::ApprovalRequired(req)) => Err(
                ToolError::Unavailable(format!("tool requires approval: {}", req.summary)),
            ),
        }
    }

    async fn invoke_outcome(
        &self,
        request: ToolRequest,
        ctx: &mut ToolContext<'_>,
    ) -> ToolExecutionOutcome {
        if let Err(reason) = self.connect().await {
            return ToolExecutionOutcome::Failed(ToolError::Unavailable(reason));
        }
        let Some(inner) = self.catalog.get(&self.spec.name) else {
            return ToolExecutionOutcome::Completed(ToolResult::new(ToolResultPart::error(
                request.call_id,
                ToolOutput::text(unknown_tool_message(&self.spec.name)),
            )));
        };
        let tool = ReconnectingTool {
            inner,
            cmd_tx: self.cmd_tx.clone(),
        };
        let requests = match tool.proposed_requests(&request) {
            Ok(requests) => requests,
            Err(error) => return ToolExecutionOutcome::Failed(error),
        };
        for permission in requests {
            match ctx.permissions.evaluate(permission.as_ref()) {
                PermissionDecision::Allow => {}
                PermissionDecision::Deny(denial) => {
                    return ToolExecutionOutcome::Failed(ToolError::PermissionDenied(denial));
                }
                PermissionDecision::RequireApproval(mut req) => {
                    req.call_id = Some(request.call_id.clone());
                    if ctx.approved_request.as_ref().map(|a| &a.id) != Some(&req.id) {
                        return ToolExecutionOutcome::Interrupted(
                            ToolInterruption::ApprovalRequired(req),
                        );
                    }
                }
            }
        }
        tool.invoke_outcome(request, ctx).await
    }
}

fn placeholder_spec(name: &ToolName) -> ToolSpec {
    ToolSpec::new(
        name.clone(),
        "Unknown tool placeholder.",
        json!({"type": "object", "additionalProperties": true}),
    )
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
            spec: placeholder_spec(name),
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
        let source = HiddenCatalogSource::new(reader, cmd_tx, KnownTools::default());

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

    struct GuardedTool {
        spec: ToolSpec,
    }

    struct GuardedRequest(agentkit_core::MetadataMap);

    impl PermissionRequest for GuardedRequest {
        fn kind(&self) -> &'static str {
            "test.guarded"
        }
        fn summary(&self) -> String {
            "guarded call".into()
        }
        fn metadata(&self) -> &agentkit_core::MetadataMap {
            &self.0
        }
        fn as_any(&self) -> &dyn std::any::Any {
            self
        }
    }

    #[async_trait]
    impl Tool for GuardedTool {
        fn spec(&self) -> &ToolSpec {
            &self.spec
        }

        fn proposed_requests(
            &self,
            _request: &ToolRequest,
        ) -> Result<Vec<Box<dyn PermissionRequest>>, ToolError> {
            Ok(vec![Box::new(GuardedRequest(Default::default()))])
        }

        async fn invoke(
            &self,
            request: ToolRequest,
            _ctx: &mut ToolContext<'_>,
        ) -> Result<ToolResult, ToolError> {
            Ok(ToolResult::new(ToolResultPart::success(
                request.call_id,
                ToolOutput::text("ran"),
            )))
        }
    }

    struct Policy(PermissionDecision);

    impl agentkit_tools_core::PermissionChecker for Policy {
        fn evaluate(&self, request: &dyn PermissionRequest) -> PermissionDecision {
            assert_eq!(request.kind(), "test.guarded");
            self.0.clone()
        }
    }

    #[tokio::test]
    async fn deferred_calls_raise_the_same_permission_requests_as_direct_ones() {
        use agentkit_core::ApprovalId;
        use agentkit_tools_core::{
            ApprovalReason, ApprovalRequest, BasicToolExecutor, OwnedToolContext, PermissionCode,
            PermissionDenial, ToolExecutor,
        };
        let name = "mcp_srv_guarded";
        let guarded = move || -> Arc<dyn Tool> {
            Arc::new(GuardedTool {
                spec: ToolSpec::new(name, "guarded", json!({"type": "object"})),
            })
        };
        let (writer, reader) = dynamic_catalog("mcp");
        let writer = Arc::new(writer);
        let known = KnownTools::default();
        known.record("srv", [name.to_string()].into());
        let (cmd_tx, mut cmd_rx) = mpsc::channel(4);
        let reconnect_writer = writer.clone();
        tokio::spawn(async move {
            while let Some(cmd) = cmd_rx.recv().await {
                if let McpCmd::ConnectForTool { reply, .. } = cmd {
                    reconnect_writer.upsert(guarded());
                    let _ = reply.send(Ok(()));
                }
            }
        });
        let executor =
            BasicToolExecutor::new([
                Arc::new(HiddenCatalogSource::new(reader, cmd_tx, known)) as Arc<dyn ToolSource>
            ]);
        let approval = ApprovalRequest {
            task_id: None,
            call_id: None,
            id: ApprovalId::new("approve-guarded"),
            request_kind: "test.guarded".into(),
            reason: ApprovalReason::PolicyRequiresConfirmation,
            summary: "guarded call".into(),
            metadata: Default::default(),
        };
        let denial = PermissionDenial {
            code: PermissionCode::CustomPolicyDenied,
            message: "denied".into(),
            metadata: Default::default(),
        };
        for decision in [
            PermissionDecision::Deny(denial),
            PermissionDecision::RequireApproval(approval),
            PermissionDecision::Allow,
        ] {
            let mut outcomes = Vec::new();
            for connected in [true, false] {
                if connected {
                    writer.upsert(guarded());
                } else {
                    writer.remove(&ToolName::new(name));
                }
                let owned = OwnedToolContext {
                    session_id: "s".into(),
                    turn_id: "t".into(),
                    metadata: Default::default(),
                    permissions: Arc::new(Policy(decision.clone())),
                    resources: Arc::new(()),
                    cancellation: None,
                    execution_scope: None,
                    approved_request: None,
                };
                let request = ToolRequest::new("call", name, json!({}), "s", "t");
                let outcome = executor.execute(request, &mut owned.borrowed()).await;
                outcomes.push(format!("{outcome:?}"));
            }
            assert_eq!(
                outcomes[0], outcomes[1],
                "a deferred call must be authorized exactly like a direct one"
            );
        }
    }
}
