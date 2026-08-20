//! Inline security consult before assistant tool execution.
//!
//! Hosted assistant tool calls used to skip the risk scanner and spend gate
//! because those live on the hook-ingest path. This wrapper asks the
//! management API whether a call may proceed and, on deny, returns a tool
//! error result without invoking the inner tool. Transport failures fail
//! open so a control-plane blip does not wedge the loop.
//!
//! [`EnforcingToolSource`] is applied twice: around the hidden MCP catalog
//! (so compose-inner MCP calls are scanned) and around the outermost clipped
//! source (native tools, compose, unknown). A shared [`DashSet`] of call ids
//! dedupes a direct MCP invocation that would otherwise consult twice.

use std::sync::Arc;

use agentkit_core::{ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    PermissionRequest, Tool, ToolCatalogEvent, ToolContext, ToolError, ToolName, ToolRequest,
    ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use dashmap::DashSet;

use crate::gram_client::GramBootstrapClient;
use crate::http_layer::TokenRegistry;

/// Wraps a [`ToolSource`] so every `invoke` consults Gram enforcement first.
pub struct EnforcingToolSource<S> {
    inner: S,
    gram_client: GramBootstrapClient,
    tokens: TokenRegistry,
    thread_id: String,
    consulted: Arc<DashSet<String>>,
}

impl<S> EnforcingToolSource<S> {
    pub fn new(
        inner: S,
        gram_client: GramBootstrapClient,
        tokens: TokenRegistry,
        thread_id: impl Into<String>,
        consulted: Arc<DashSet<String>>,
    ) -> Self {
        Self {
            inner,
            gram_client,
            tokens,
            thread_id: thread_id.into(),
            consulted,
        }
    }
}

impl<S> ToolSource for EnforcingToolSource<S>
where
    S: ToolSource,
{
    fn specs(&self) -> Vec<ToolSpec> {
        self.inner.specs()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        self.inner.get(name).map(|inner| {
            Arc::new(EnforcingTool {
                inner,
                gram_client: self.gram_client.clone(),
                tokens: self.tokens.clone(),
                thread_id: self.thread_id.clone(),
                consulted: Arc::clone(&self.consulted),
            }) as Arc<dyn Tool>
        })
    }

    fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
        self.inner.drain_catalog_events()
    }
}

struct EnforcingTool {
    inner: Arc<dyn Tool>,
    gram_client: GramBootstrapClient,
    tokens: TokenRegistry,
    thread_id: String,
    consulted: Arc<DashSet<String>>,
}

#[async_trait]
impl Tool for EnforcingTool {
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
        if let Some(denied) = consult_tool_invocation(
            &self.gram_client,
            &self.tokens,
            &self.thread_id,
            &self.consulted,
            &request,
        )
        .await
        {
            return Ok(denied);
        }
        self.inner.invoke(request, ctx).await
    }
}

/// Returns a deny tool result when enforcement blocks the call, or `None`
/// when the call may proceed (including fail-open on consult errors).
async fn consult_tool_invocation(
    client: &GramBootstrapClient,
    tokens: &TokenRegistry,
    thread_id: &str,
    consulted: &DashSet<String>,
    request: &ToolRequest,
) -> Option<ToolResult> {
    let call_id = request.call_id.0.clone();
    if !call_id.is_empty() && !consulted.insert(call_id.clone()) {
        return None;
    }

    match client
        .consult_tool_call(
            thread_id,
            request.tool_name.0.as_str(),
            &request.input,
            &call_id,
            tokens,
        )
        .await
    {
        Ok(response) if response.is_deny() => Some(ToolResult::new(ToolResultPart::error(
            request.call_id.clone(),
            ToolOutput::text(response.deny_message()),
        ))),
        Ok(_) => None,
        Err(error) => {
            tracing::warn!(
                error = %error,
                thread_id,
                tool_name = %request.tool_name.0,
                "assistant tool-call consult failed; allowing"
            );
            None
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use crate::gram_client::GramBootstrapClient;
    use crate::http_layer::build_bootstrap_client;
    use agentkit_core::ToolCallId;
    use axum::Json;
    use axum::extract::State;
    use axum::routing::post;
    use serde_json::{Value, json};
    use std::net::SocketAddr;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use tokio::sync::oneshot;

    struct EchoTool {
        spec: ToolSpec,
        invocations: Arc<AtomicUsize>,
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
            self.invocations.fetch_add(1, Ordering::SeqCst);
            Ok(ToolResult::new(ToolResultPart::success(
                request.call_id,
                ToolOutput::text("echo"),
            )))
        }
    }

    struct StaticSource {
        tool: Arc<dyn Tool>,
    }

    impl ToolSource for StaticSource {
        fn specs(&self) -> Vec<ToolSpec> {
            vec![self.tool.spec().clone()]
        }

        fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
            if name.0 == self.tool.spec().name.0 {
                Some(Arc::clone(&self.tool))
            } else {
                None
            }
        }

        fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
            Vec::new()
        }
    }

    #[derive(Clone)]
    struct MockState {
        decision: &'static str,
        message: &'static str,
        status: u16,
        hits: Arc<AtomicUsize>,
    }

    async fn spawn_consult_server(
        decision: &'static str,
        message: &'static str,
        status: u16,
    ) -> (String, oneshot::Sender<()>, Arc<AtomicUsize>) {
        let hits = Arc::new(AtomicUsize::new(0));
        let state = MockState {
            decision,
            message,
            status,
            hits: Arc::clone(&hits),
        };
        let app = axum::Router::new()
            .route(
                "/rpc/assistants.consultToolCall",
                post(
                    |State(state): State<MockState>, Json(_body): Json<Value>| async move {
                        state.hits.fetch_add(1, Ordering::SeqCst);
                        let body = json!({
                            "decision": state.decision,
                            "message": state.message,
                        });
                        (
                            axum::http::StatusCode::from_u16(state.status)
                                .unwrap_or(axum::http::StatusCode::OK),
                            Json(body),
                        )
                    },
                ),
            )
            .with_state(state);

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr: SocketAddr = listener.local_addr().unwrap();
        let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
        tokio::spawn(async move {
            axum::serve(listener, app)
                .with_graceful_shutdown(async {
                    let _ = shutdown_rx.await;
                })
                .await
                .unwrap();
        });
        (format!("http://{addr}"), shutdown_tx, hits)
    }

    fn client(base_url: String) -> GramBootstrapClient {
        GramBootstrapClient::new(base_url, build_bootstrap_client(reqwest::Client::new()))
    }

    fn tool_request(name: &str, call_id: &str) -> ToolRequest {
        ToolRequest {
            tool_name: ToolName::new(name),
            call_id: ToolCallId::new(call_id),
            input: json!({"code": "1"}),
        }
    }

    #[tokio::test]
    async fn deny_returns_error_result_without_invoking_inner() {
        let (base, shutdown, hits) =
            spawn_consult_server("deny", "Speakeasy blocked this tool call", 200).await;
        let invocations = Arc::new(AtomicUsize::new(0));
        let inner = Arc::new(EchoTool {
            spec: ToolSpec::new(
                "bun_run",
                "runs bun",
                json!({"type": "object", "properties": {}}),
            ),
            invocations: Arc::clone(&invocations),
        });
        let source = EnforcingToolSource::new(
            StaticSource { tool: inner },
            client(base),
            TokenRegistry::new("token"),
            "thread-1",
            Arc::new(DashSet::new()),
        );
        let tool = source.get(&ToolName::new("bun_run")).expect("tool");
        let mut ctx = unused_tool_context();
        let result = tool
            .invoke(tool_request("bun_run", "call_deny"), &mut ctx)
            .await
            .expect("invoke");
        match result.result.output {
            ToolOutput::Text(text) => assert!(text.contains("Speakeasy blocked this tool call")),
            other => panic!("expected text deny, got {other:?}"),
        }
        assert_eq!(invocations.load(Ordering::SeqCst), 0);
        assert_eq!(hits.load(Ordering::SeqCst), 1);
        let _ = shutdown.send(());
    }

    #[tokio::test]
    async fn allow_invokes_inner() {
        let (base, shutdown, hits) = spawn_consult_server("allow", "", 200).await;
        let invocations = Arc::new(AtomicUsize::new(0));
        let inner = Arc::new(EchoTool {
            spec: ToolSpec::new(
                "bun_run",
                "runs bun",
                json!({"type": "object", "properties": {}}),
            ),
            invocations: Arc::clone(&invocations),
        });
        let source = EnforcingToolSource::new(
            StaticSource { tool: inner },
            client(base),
            TokenRegistry::new("token"),
            "thread-1",
            Arc::new(DashSet::new()),
        );
        let tool = source.get(&ToolName::new("bun_run")).expect("tool");
        let mut ctx = unused_tool_context();
        let result = tool
            .invoke(tool_request("bun_run", "call_allow"), &mut ctx)
            .await
            .expect("invoke");
        match result.result.output {
            ToolOutput::Text(text) => assert_eq!(text, "echo"),
            other => panic!("expected echo, got {other:?}"),
        }
        assert_eq!(invocations.load(Ordering::SeqCst), 1);
        assert_eq!(hits.load(Ordering::SeqCst), 1);
        let _ = shutdown.send(());
    }

    #[tokio::test]
    async fn server_error_fails_open_and_invokes_inner() {
        let (base, shutdown, hits) = spawn_consult_server("allow", "", 500).await;
        let invocations = Arc::new(AtomicUsize::new(0));
        let inner = Arc::new(EchoTool {
            spec: ToolSpec::new(
                "bun_run",
                "runs bun",
                json!({"type": "object", "properties": {}}),
            ),
            invocations: Arc::clone(&invocations),
        });
        let source = EnforcingToolSource::new(
            StaticSource { tool: inner },
            client(base),
            TokenRegistry::new("token"),
            "thread-1",
            Arc::new(DashSet::new()),
        );
        let tool = source.get(&ToolName::new("bun_run")).expect("tool");
        let mut ctx = unused_tool_context();
        let result = tool
            .invoke(tool_request("bun_run", "call_5xx"), &mut ctx)
            .await
            .expect("invoke");
        match result.result.output {
            ToolOutput::Text(text) => assert_eq!(text, "echo"),
            other => panic!("expected echo, got {other:?}"),
        }
        assert_eq!(invocations.load(Ordering::SeqCst), 1);
        assert!(hits.load(Ordering::SeqCst) >= 1);
        let _ = shutdown.send(());
    }

    #[tokio::test]
    async fn duplicate_call_id_consults_once() {
        let (base, shutdown, hits) = spawn_consult_server("allow", "", 200).await;
        let invocations = Arc::new(AtomicUsize::new(0));
        let inner = Arc::new(EchoTool {
            spec: ToolSpec::new(
                "bun_run",
                "runs bun",
                json!({"type": "object", "properties": {}}),
            ),
            invocations: Arc::clone(&invocations),
        });
        let consulted = Arc::new(DashSet::new());
        let source = EnforcingToolSource::new(
            StaticSource {
                tool: Arc::clone(&inner) as Arc<dyn Tool>,
            },
            client(base),
            TokenRegistry::new("token"),
            "thread-1",
            Arc::clone(&consulted),
        );
        let tool = source.get(&ToolName::new("bun_run")).expect("tool");
        let mut ctx = unused_tool_context();
        tool.invoke(tool_request("bun_run", "call_dup"), &mut ctx)
            .await
            .unwrap();
        tool.invoke(tool_request("bun_run", "call_dup"), &mut ctx)
            .await
            .unwrap();
        assert_eq!(hits.load(Ordering::SeqCst), 1);
        assert_eq!(invocations.load(Ordering::SeqCst), 2);
        let _ = shutdown.send(());
    }

    fn unused_tool_context() -> ToolContext<'static> {
        ToolContext::default()
    }
}
