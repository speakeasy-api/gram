use std::collections::HashMap;
use std::sync::{Arc, RwLock};

use agentkit_http::Http;
use agentkit_mcp::{
    ClientJsonRpcMessage, McpHttpClient, McpSseStream, McpStreamableHttpError,
    McpStreamableHttpPostResponse,
};
use async_trait::async_trait;
use http::{HeaderMap, HeaderName, HeaderValue};
use reqwest_middleware::{ClientBuilder, Middleware, Next};
use reqwest_retry::RetryTransientMiddleware;
use reqwest_retry::policies::ExponentialBackoff;
use rmcp::transport::streamable_http_client::StreamableHttpClient as RmcpStreamableHttpClient;
use thiserror::Error;

use crate::errors::RunnerError;

const HTTP_MAX_RETRIES: u32 = 3;

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

    fn current(&self) -> Result<String, RunnerError> {
        Ok(self
            .inner
            .read()
            .map_err(|_| RunnerError::Loop("token registry read lock poisoned".into()))?
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
            .inner
            .read()
            .map_err(|_| reqwest_middleware::Error::middleware(MiddlewareError::LockPoisoned))?
            .clone();
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
    let policy = ExponentialBackoff::builder().build_with_max_retries(HTTP_MAX_RETRIES);
    RetryTransientMiddleware::new_with_policy(policy).with_retry_log_level(tracing::Level::INFO)
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
