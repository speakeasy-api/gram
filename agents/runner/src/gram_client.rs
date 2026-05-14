use std::time::Duration;

use reqwest_middleware::ClientWithMiddleware;
use serde::Serialize;
use thiserror::Error;

use crate::http_layer::TokenRegistry;
use crate::wire::ThreadBootstrap;

const BOOTSTRAP_PATH: &str = "/rpc/assistants.getThreadBootstrap";
const BOOTSTRAP_TIMEOUT: Duration = Duration::from_secs(15);

/// Lightweight client used by the runner to pull a per-thread bootstrap
/// from the management API. Uses the host's shared `TokenRegistry` so the
/// bearer always reflects the most recent rotation pushed via /turn. The
/// underlying client carries `RetryTransientMiddleware` so transient 5xx /
/// network errors are retried with exponential backoff before the first
/// turn for an assistant fails.
#[derive(Clone)]
pub struct GramBootstrapClient {
    base_url: String,
    http: ClientWithMiddleware,
    tokens: TokenRegistry,
}

#[derive(Debug, Error)]
pub enum GramClientError {
    #[error("send bootstrap request: {0}")]
    Send(#[from] reqwest_middleware::Error),

    #[error("read bootstrap token")]
    Token,

    #[error("read bootstrap body: {0}")]
    Read(#[from] reqwest::Error),

    #[error("bootstrap request failed: status={status} body={body}")]
    Status { status: u16, body: String },

    #[error("decode bootstrap response: {0}")]
    Decode(#[from] serde_json::Error),
}

#[derive(Serialize)]
struct BootstrapRequest<'a> {
    thread_id: &'a str,
}

impl GramBootstrapClient {
    pub fn new(base_url: String, http: ClientWithMiddleware, tokens: TokenRegistry) -> Self {
        Self {
            base_url,
            http,
            tokens,
        }
    }

    /// Fetches the bootstrap blob for a thread. Caller is responsible for
    /// ensuring this is called at most once per thread per VM lifetime
    /// (the runtime's `OnceCell` guard handles that for the live path).
    pub async fn fetch_bootstrap(
        &self,
        thread_id: &str,
    ) -> Result<ThreadBootstrap, GramClientError> {
        let url = format!("{}{}", self.base_url.trim_end_matches('/'), BOOTSTRAP_PATH);
        let bearer = self.tokens.current().map_err(|_| GramClientError::Token)?;

        let resp = self
            .http
            .post(&url)
            .timeout(BOOTSTRAP_TIMEOUT)
            .bearer_auth(&bearer)
            .json(&BootstrapRequest { thread_id })
            .send()
            .await?;

        let status = resp.status();
        let body = resp.text().await?;
        if !status.is_success() {
            return Err(GramClientError::Status {
                status: status.as_u16(),
                body,
            });
        }
        let bootstrap: ThreadBootstrap = serde_json::from_str(&body)?;
        Ok(bootstrap)
    }
}
