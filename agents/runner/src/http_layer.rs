use std::sync::{Arc, RwLock};

use agentkit_http::Http;
use async_trait::async_trait;
use reqwest_middleware::{ClientBuilder, Middleware, Next};
use reqwest_retry::RetryTransientMiddleware;
use reqwest_retry::policies::ExponentialBackoff;
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

#[derive(Clone, Debug)]
pub struct StaticHeaders {
    headers: http::HeaderMap,
}

impl StaticHeaders {
    pub fn new(headers: http::HeaderMap) -> Self {
        Self { headers }
    }
}

#[async_trait]
impl Middleware for StaticHeaders {
    async fn handle(
        &self,
        mut req: reqwest::Request,
        extensions: &mut http::Extensions,
        next: Next<'_>,
    ) -> reqwest_middleware::Result<reqwest::Response> {
        for (name, value) in &self.headers {
            req.headers_mut().insert(name.clone(), value.clone());
        }
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

pub fn build_http_with_static(
    client: reqwest::Client,
    registry: TokenRegistry,
    static_headers: http::HeaderMap,
) -> Http {
    let client = ClientBuilder::new(client)
        .with(retry_middleware())
        .with(StaticHeaders::new(static_headers))
        .with(registry)
        .build();
    Http::new(client)
}

fn retry_middleware() -> RetryTransientMiddleware<ExponentialBackoff> {
    let policy = ExponentialBackoff::builder().build_with_max_retries(HTTP_MAX_RETRIES);
    RetryTransientMiddleware::new_with_policy(policy).with_retry_log_level(tracing::Level::INFO)
}
