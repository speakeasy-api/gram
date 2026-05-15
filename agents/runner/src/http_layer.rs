use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::Duration;

use agentkit_http::Http;
use agentkit_mcp::{
    ClientJsonRpcMessage, McpHttpClient, McpSseStream, McpStreamableHttpError,
    McpStreamableHttpPostResponse,
};
use async_trait::async_trait;
use http::{HeaderMap, HeaderName, HeaderValue};
use reqwest_middleware::{ClientBuilder, ClientWithMiddleware, Middleware, Next};
use reqwest_retry::RetryTransientMiddleware;
use reqwest_retry::policies::ExponentialBackoff;
use rmcp::transport::streamable_http_client::StreamableHttpClient as RmcpStreamableHttpClient;
use thiserror::Error;

use crate::errors::RunnerError;

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
    #[error("token registry lock poisoned")]
    LockPoisoned,

    #[error("invalid bearer token header value: {0}")]
    InvalidTokenHeader(#[from] http::header::InvalidHeaderValue),
}

#[derive(Clone, Debug)]
pub struct TokenRegistry {
    inner: Arc<RwLock<String>>,
}

tokio::task_local! {
    /// Per-thread registry scoped onto the spawning task. Outbound HTTP
    /// (chat completions, MCP, bootstrap fetch) prefers this over the
    /// host-shared fallback so a shared VM serving multiple threads still
    /// authenticates each request under the calling thread's JWT — the
    /// ThreadID claim then propagates through to platform tools that key
    /// off `principal.ThreadID` (wake, memory, telemetry).
    pub static THREAD_TOKEN: TokenRegistry;
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
            .map_err(|_| RunnerError::Loop("token registry write lock poisoned".into()))?;
        *slot = next.into();
        Ok(())
    }

    fn read_local(&self) -> Result<String, RunnerError> {
        Ok(self
            .inner
            .read()
            .map_err(|_| RunnerError::Loop("token registry read lock poisoned".into()))?
            .clone())
    }

    pub fn current(&self) -> Result<String, RunnerError> {
        match THREAD_TOKEN.try_with(|r| r.read_local()) {
            Ok(res) => res,
            Err(_) => self.read_local(),
        }
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
        let token_result = match THREAD_TOKEN.try_with(|r| r.read_local()) {
            Ok(res) => res,
            Err(_) => self.read_local(),
        };
        let token = token_result
            .map_err(|_| reqwest_middleware::Error::middleware(MiddlewareError::LockPoisoned))?;
        let value = http::HeaderValue::try_from(format!("Bearer {token}"))
            .map_err(|e| reqwest_middleware::Error::middleware(MiddlewareError::from(e)))?;
        req.headers_mut().insert(http::header::AUTHORIZATION, value);
        next.run(req, extensions).await
    }
}

pub fn build_http(client: reqwest::Client, registry: TokenRegistry) -> Http {
    let client = ClientBuilder::new(client)
        .with(retry_middleware())
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
    ClientBuilder::new(client).with(retry_middleware()).build()
}

/// `McpHttpClient` impl that mints a fresh bearer token per request from a
/// shared [`TokenRegistry`]. Replaces the static `bearer_token` path in
/// [`agentkit_mcp::StreamableHttpTransportConfig`] so token rotation does
/// not require a reconnect.
pub struct McpRotatingClient {
    inner: reqwest::Client,
    tokens: TokenRegistry,
    static_headers: HeaderMap,
}

impl McpRotatingClient {
    pub fn new(inner: reqwest::Client, tokens: TokenRegistry, static_headers: HeaderMap) -> Self {
        Self {
            inner,
            tokens,
            static_headers,
        }
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

    fn current_token(&self) -> Option<String> {
        self.tokens.current().ok()
    }
}

#[async_trait]
impl McpHttpClient for McpRotatingClient {
    async fn post_message(
        &self,
        uri: Arc<str>,
        message: ClientJsonRpcMessage,
        session_id: Option<Arc<str>>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<McpStreamableHttpPostResponse, McpStreamableHttpError<reqwest::Error>> {
        let token = self.current_token();
        let headers = self.merged_headers(custom_headers);
        RmcpStreamableHttpClient::post_message(
            &self.inner,
            uri,
            message,
            session_id,
            token,
            headers,
        )
        .await
    }

    async fn delete_session(
        &self,
        uri: Arc<str>,
        session_id: Arc<str>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<(), McpStreamableHttpError<reqwest::Error>> {
        let token = self.current_token();
        let headers = self.merged_headers(custom_headers);
        RmcpStreamableHttpClient::delete_session(&self.inner, uri, session_id, token, headers).await
    }

    async fn get_stream(
        &self,
        uri: Arc<str>,
        session_id: Arc<str>,
        last_event_id: Option<String>,
        _auth_header: Option<String>,
        custom_headers: HashMap<HeaderName, HeaderValue>,
    ) -> Result<McpSseStream, McpStreamableHttpError<reqwest::Error>> {
        let token = self.current_token();
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
