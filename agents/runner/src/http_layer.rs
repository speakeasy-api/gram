use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::Duration;

use agentkit_http::Http;
use agentkit_mcp::{
    ClientJsonRpcMessage, McpHttpClient, McpSseStream, McpStreamableHttpError,
    McpStreamableHttpPostResponse,
};
use async_trait::async_trait;
use futures::StreamExt;
use http::{HeaderMap, HeaderName, HeaderValue};
use reqwest_middleware::{ClientBuilder, ClientWithMiddleware, Middleware, Next};
use reqwest_retry::RetryTransientMiddleware;
use reqwest_retry::policies::ExponentialBackoff;
use rmcp::model::{ClientRequest, ServerJsonRpcMessage, ServerResult};
use rmcp::transport::streamable_http_client::StreamableHttpClient as RmcpStreamableHttpClient;
use thiserror::Error;

use crate::errors::RunnerError;
use crate::telemetry::McpTraceHeaders;

// Liberal retry bounds for every runner-originated request: chat
// completions, MCP, /threads/turn, bootstrap. A turn that bubbles a
// transient gateway blip back to the user is more painful than waiting
// up to ~30s for recovery, so we cap at 7 attempts with 250ms→30s
// exponential bounds across the board.
const HTTP_MAX_RETRIES: u32 = 7;
const HTTP_RETRY_MIN: Duration = Duration::from_millis(250);
const HTTP_RETRY_MAX: Duration = Duration::from_secs(30);

#[derive(Debug, Error)]
enum MiddlewareError {
    #[error("invalid bearer token header value: {0}")]
    InvalidTokenHeader(#[from] http::header::InvalidHeaderValue),
}

/// Bearer slot for one assistant thread. Every outbound request the thread
/// makes (chat completions, MCP, bootstrap fetch) authenticates against
/// this slot, so the ThreadID claim minted on `/threads/turn` propagates
/// to platform tools that key off `principal.ThreadID`.
#[derive(Clone, Debug)]
pub struct TokenRegistry {
    inner: Arc<RwLock<String>>,
}

impl TokenRegistry {
    pub fn new(initial: impl Into<String>) -> Self {
        Self {
            inner: Arc::new(RwLock::new(initial.into())),
        }
    }

    pub fn rotate(&self, next: impl Into<String>) -> Result<(), RunnerError> {
        let mut slot = self
            .inner
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        *slot = next.into();
        Ok(())
    }

    pub fn current(&self) -> Result<String, RunnerError> {
        Ok(self
            .inner
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone())
    }
}

#[async_trait]
impl Middleware for TokenRegistry {
    async fn handle(
        &self,
        mut req: reqwest::Request,
        extensions: &mut http::Extensions,
        next: Next<'_>,
    ) -> reqwest_middleware::Result<reqwest::Response> {
        let token = self
            .current()
            .map_err(reqwest_middleware::Error::middleware)?;
        let value = http::HeaderValue::try_from(format!("Bearer {token}"))
            .map_err(|e| reqwest_middleware::Error::middleware(MiddlewareError::from(e)))?;
        req.headers_mut().insert(http::header::AUTHORIZATION, value);
        next.run(req, extensions).await
    }
}

/// Stamps the current span's W3C trace context on every outbound request.
/// Runs inside each request's future, so the context is the span the caller
/// awaited the request under (for example agentkit's `chat` span).
#[derive(Clone, Copy, Debug, Default)]
pub struct TraceContextMiddleware;

#[async_trait]
impl Middleware for TraceContextMiddleware {
    async fn handle(
        &self,
        mut req: reqwest::Request,
        extensions: &mut http::Extensions,
        next: Next<'_>,
    ) -> reqwest_middleware::Result<reqwest::Response> {
        crate::telemetry::inject_current_trace_context(req.headers_mut());
        next.run(req, extensions).await
    }
}

pub fn build_http(client: reqwest::Client, registry: TokenRegistry) -> Http {
    let client = ClientBuilder::new(client)
        .with(retry_middleware())
        .with(TraceContextMiddleware)
        .with(registry)
        .build();
    Http::new(client)
}

fn retry_middleware() -> RetryTransientMiddleware<ExponentialBackoff> {
    let policy = ExponentialBackoff::builder()
        .retry_bounds(HTTP_RETRY_MIN, HTTP_RETRY_MAX)
        .build_with_max_retries(HTTP_MAX_RETRIES);
    RetryTransientMiddleware::new_with_policy(policy).with_retry_log_level(tracing::Level::INFO)
}

/// Wraps a base reqwest client with the shared liberal retry policy. Used by
/// the management-API bootstrap call so a transient 5xx or network blip does
/// not strand the VM's first turn for an assistant.
pub fn build_bootstrap_client(client: reqwest::Client) -> ClientWithMiddleware {
    ClientBuilder::new(client)
        .with(retry_middleware())
        .with(TraceContextMiddleware)
        .build()
}

/// `McpHttpClient` impl that sends one fixed bearer for the lifetime of a
/// transport. Each MCP session captures the credential of the invocation that
/// opened it, so late protocol traffic — a slow discovery POST or the session
/// DELETE that cleans it up — can never carry a later invocation's bearer to
/// an endpoint that invocation did not configure.
pub struct McpSessionClient {
    inner: reqwest::Client,
    token: String,
    static_headers: HeaderMap,
    instructions_slot: Option<Arc<RwLock<Option<String>>>>,
}

impl McpSessionClient {
    pub fn new(inner: reqwest::Client, token: String, static_headers: HeaderMap) -> Self {
        Self {
            inner,
            token,
            static_headers,
            instructions_slot: None,
        }
    }

    pub fn with_instructions_slot(mut self, slot: Arc<RwLock<Option<String>>>) -> Self {
        self.instructions_slot = Some(slot);
        self
    }

    fn merged_headers(
        &self,
        mut custom: HashMap<HeaderName, HeaderValue>,
    ) -> HashMap<HeaderName, HeaderValue> {
        for (name, value) in self.static_headers.iter() {
            custom.entry(name.clone()).or_insert_with(|| value.clone());
        }
        custom
    }

    fn bearer(&self) -> Option<String> {
        Some(self.token.clone()).filter(|t| !t.is_empty())
    }
}

#[async_trait]
impl McpHttpClient for McpSessionClient {
    async fn post_message(
        &self,
        uri: Arc<str>,
        message: ClientJsonRpcMessage,
        session_id: Option<Arc<str>>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<McpStreamableHttpPostResponse, McpStreamableHttpError<reqwest::Error>> {
        let token = self.bearer();
        let mut headers = self.merged_headers(custom_headers);
        let capture_instructions = self.instructions_slot.is_some()
            && matches!(
                &message,
                ClientJsonRpcMessage::Request(req)
                    if matches!(req.request, ClientRequest::InitializeRequest(_))
            );
        // Tool calls carry the trace context agentkit's request decorator
        // captured for them, so the server's MCP request links to the runner
        // trace.
        if let Some(McpTraceHeaders(trace)) = call_trace_headers(&message) {
            headers.extend(trace.clone());
        }
        let response = RmcpStreamableHttpClient::post_message(
            &self.inner,
            uri,
            message,
            session_id,
            token,
            headers,
        )
        .await?;
        if !capture_instructions {
            return Ok(response);
        }
        let Some(slot) = self.instructions_slot.clone() else {
            return Ok(response);
        };
        Ok(capture_initialize_instructions(response, slot))
    }

    async fn delete_session(
        &self,
        uri: Arc<str>,
        session_id: Arc<str>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<(), McpStreamableHttpError<reqwest::Error>> {
        let token = self.bearer();
        let headers = self.merged_headers(custom_headers);
        RmcpStreamableHttpClient::delete_session(&self.inner, uri, session_id, token, headers).await
    }

    async fn get_stream(
        &self,
        uri: Arc<str>,
        session_id: Option<Arc<str>>,
        last_event_id: Option<String>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<McpSseStream, McpStreamableHttpError<reqwest::Error>> {
        let token = self.bearer();
        let headers = self.merged_headers(custom_headers);
        RmcpStreamableHttpClient::get_stream(
            &self.inner,
            uri,
            session_id,
            last_event_id,
            token,
            headers,
        )
        .await
    }
}

/// The trace headers attached to a `tools/call` request, if any.
fn call_trace_headers(message: &ClientJsonRpcMessage) -> Option<&McpTraceHeaders> {
    match message {
        ClientJsonRpcMessage::Request(request) => match &request.request {
            ClientRequest::CallToolRequest(call) => call.extensions.get::<McpTraceHeaders>(),
            _ => None,
        },
        _ => None,
    }
}

fn extract_initialize_instructions(message: &ServerJsonRpcMessage) -> Option<String> {
    let ServerJsonRpcMessage::Response(response) = message else {
        return None;
    };
    let ServerResult::InitializeResult(init) = &response.result else {
        return None;
    };
    init.instructions
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_string)
}

fn store_instructions(slot: &Arc<RwLock<Option<String>>>, instructions: Option<String>) {
    *slot
        .write()
        .unwrap_or_else(std::sync::PoisonError::into_inner) = instructions;
}

fn capture_initialize_instructions(
    response: McpStreamableHttpPostResponse,
    slot: Arc<RwLock<Option<String>>>,
) -> McpStreamableHttpPostResponse {
    match response {
        McpStreamableHttpPostResponse::Json(message, session_id) => {
            store_instructions(&slot, extract_initialize_instructions(&message));
            McpStreamableHttpPostResponse::Json(message, session_id)
        }
        McpStreamableHttpPostResponse::Sse(stream, session_id) => {
            store_instructions(&slot, None);
            let wrapped = stream.inspect(move |event| {
                if let Ok(sse) = event
                    && let Some(data) = sse.data.as_deref()
                    && let Ok(message) = serde_json::from_str::<ServerJsonRpcMessage>(data)
                    && let Some(instructions) = extract_initialize_instructions(&message)
                {
                    store_instructions(&slot, Some(instructions));
                }
            });
            McpStreamableHttpPostResponse::Sse(Box::pin(wrapped), session_id)
        }
        other => other,
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::panic)]
mod tests {
    use super::*;

    #[test]
    fn only_call_tool_requests_carry_trace_headers() {
        use rmcp::model::{
            CallToolRequest, CallToolRequestParams, JsonRpcRequest, NumberOrString, PingRequest,
        };
        let mut trace = HashMap::new();
        trace.insert(
            HeaderName::from_static("traceparent"),
            HeaderValue::from_static("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
        );
        let mut call = CallToolRequest::new(CallToolRequestParams::new("platform_overview"));
        call.extensions.insert(McpTraceHeaders(trace.clone()));
        let message = |request| {
            ClientJsonRpcMessage::Request(JsonRpcRequest {
                jsonrpc: Default::default(),
                id: NumberOrString::Number(1),
                request,
            })
        };
        let call = message(ClientRequest::CallToolRequest(call));
        assert_eq!(
            call_trace_headers(&call).map(|McpTraceHeaders(h)| h),
            Some(&trace)
        );
        assert!(
            call_trace_headers(&message(ClientRequest::PingRequest(PingRequest::default())))
                .is_none()
        );
    }

    #[test]
    fn scalar_bearer_recovers_after_lock_poison() {
        let tokens = TokenRegistry::new("opaque-original");
        let poison = tokens.clone();
        let _ = std::thread::spawn(move || {
            let _guard = poison.inner.write().unwrap();
            panic!("test-only poison");
        })
        .join();
        assert_eq!(tokens.current().unwrap(), "opaque-original");
        tokens.rotate("opaque-next").unwrap();
        assert_eq!(tokens.current().unwrap(), "opaque-next");
    }

    #[tokio::test]
    async fn capture_initialize_instructions_handles_sse_and_blank_values() {
        use axum::{Router, http::header::CONTENT_TYPE, routing::post};
        use rmcp::model::{
            ClientCapabilities, Implementation, InitializeRequest, InitializeRequestParams,
            JsonRpcRequest, NumberOrString, ProtocolVersion,
        };
        use std::sync::Mutex;

        let next_instructions: Arc<Mutex<Option<String>>> =
            Arc::new(Mutex::new(Some("  Use list_servers first.  \n".into())));
        let server_instructions = Arc::clone(&next_instructions);

        let app = Router::new().route(
            "/sse-gateway",
            post(move || {
                let instructions = server_instructions.lock().unwrap().clone();
                async move {
                    let body = serde_json::json!({
                        "jsonrpc": "2.0",
                        "id": 1,
                        "result": {
                            "protocolVersion": "2025-03-26",
                            "capabilities": { "tools": {} },
                            "serverInfo": { "name": "project-gateway", "version": "1.0.0" },
                            "instructions": instructions,
                        }
                    });
                    (
                        [(CONTENT_TYPE, "text/event-stream")],
                        format!("event: message\ndata: {body}\n\n"),
                    )
                }
            }),
        );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let init_msg = || {
            let mut params = InitializeRequestParams::new(
                ClientCapabilities::default(),
                Implementation::new("runner-test", "1.0.0"),
            );
            params.protocol_version = ProtocolVersion::V_2025_03_26;
            ClientJsonRpcMessage::Request(JsonRpcRequest {
                jsonrpc: Default::default(),
                id: NumberOrString::Number(1),
                request: ClientRequest::InitializeRequest(InitializeRequest::new(params)),
            })
        };

        let slot = Arc::new(RwLock::new(None));
        let client = McpSessionClient::new(reqwest::Client::new(), "tok".into(), HeaderMap::new())
            .with_instructions_slot(Arc::clone(&slot));
        let uri: Arc<str> = format!("http://{addr}/sse-gateway").into();

        // 1. SSE initialize response trims and stores non-empty instructions.
        let resp = client
            .post_message(Arc::clone(&uri), init_msg(), None, None, HashMap::new())
            .await
            .unwrap();
        let McpStreamableHttpPostResponse::Sse(mut stream, _) = resp else {
            panic!("expected SSE response");
        };
        while stream.next().await.is_some() {}
        assert_eq!(
            slot.read().unwrap().as_deref(),
            Some("Use list_servers first.")
        );

        // 2. SSE initialize response with whitespace-only instructions clears
        // any prior value to None.
        *next_instructions.lock().unwrap() = Some("  \t\n ".into());
        let resp = client
            .post_message(Arc::clone(&uri), init_msg(), None, None, HashMap::new())
            .await
            .unwrap();
        let McpStreamableHttpPostResponse::Sse(mut stream, _) = resp else {
            panic!("expected SSE response");
        };
        while stream.next().await.is_some() {}
        assert_eq!(slot.read().unwrap().as_deref(), None);

        // 3. SSE initialize response with empty instructions keeps slot None.
        store_instructions(&slot, Some("stale".into()));
        *next_instructions.lock().unwrap() = Some(String::new());
        let resp = client
            .post_message(uri, init_msg(), None, None, HashMap::new())
            .await
            .unwrap();
        let McpStreamableHttpPostResponse::Sse(mut stream, _) = resp else {
            panic!("expected SSE response");
        };
        while stream.next().await.is_some() {}
        assert_eq!(slot.read().unwrap().as_deref(), None);

        server_task.abort();
    }
}
