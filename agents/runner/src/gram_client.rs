use std::time::Duration;

use reqwest_middleware::ClientWithMiddleware;
use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::http_layer::TokenRegistry;
use crate::wire::{RunnerMessage, ThreadBootstrap};

const BOOTSTRAP_PATH: &str = "/rpc/assistants.getThreadBootstrap";
const CREATE_MCP_AUTH_FLOW_PATH: &str = "/rpc/assistantMcpAuth.create";
const RECORD_COMPACTED_GENERATION_PATH: &str = "/rpc/assistants.recordCompactedGeneration";
const CONSULT_TOOL_CALL_PATH: &str = "/rpc/assistants.consultToolCall";
const BOOTSTRAP_TIMEOUT: Duration = Duration::from_secs(15);
const CONSULT_TIMEOUT: Duration = Duration::from_secs(30);
// Compaction persistence runs off the loop's critical path, so a longer
// budget is fine; the post-compaction call body can be sizable and the
// server walks all rows in a single transaction.
const RECORD_COMPACTION_TIMEOUT: Duration = Duration::from_secs(30);

/// Lightweight client used by the runner to pull a per-thread bootstrap
/// from the management API. The underlying client carries
/// `RetryTransientMiddleware` so transient 5xx / network errors are
/// retried with exponential backoff before the first turn for an
/// assistant fails.
#[derive(Clone)]
pub struct GramBootstrapClient {
    base_url: String,
    http: ClientWithMiddleware,
}

#[derive(Debug, Error)]
pub enum GramClientError {
    #[error("send request: {0}")]
    Send(#[from] reqwest_middleware::Error),

    #[error("read token")]
    Token,

    #[error("read response body: {0}")]
    Read(#[from] reqwest::Error),

    #[error("request failed: status={status} body={body}")]
    Status { status: u16, body: String },

    #[error("decode response: {0}")]
    Decode(#[from] serde_json::Error),
}

#[derive(Serialize)]
struct BootstrapRequest<'a> {
    thread_id: &'a str,
}

#[derive(Serialize)]
struct CreateMcpAuthFlowRequest<'a> {
    thread_id: &'a str,
    server_id: &'a str,
    url: &'a str,
}

#[derive(Serialize)]
struct RecordCompactedGenerationRequest<'a> {
    thread_id: &'a str,
    messages: &'a [RunnerMessage],
}

#[derive(Serialize)]
struct ConsultToolCallRequest<'a> {
    thread_id: &'a str,
    tool_name: &'a str,
    tool_input: &'a serde_json::Value,
    tool_call_id: &'a str,
}

#[derive(Debug, Deserialize)]
pub struct ConsultToolCallResponse {
    pub decision: String,
    #[serde(default)]
    pub message: String,
}

impl ConsultToolCallResponse {
    pub fn is_deny(&self) -> bool {
        self.decision.eq_ignore_ascii_case("deny")
    }

    pub fn deny_message(&self) -> String {
        let trimmed = self.message.trim();
        if trimmed.is_empty() {
            "Request denied by Speakeasy policy.".to_string()
        } else {
            trimmed.to_string()
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct CreateMcpAuthFlowResponse {
    pub server_id: String,
    pub mcp_slug: String,
    pub auth_url: String,
}

impl GramBootstrapClient {
    pub fn new(base_url: String, http: ClientWithMiddleware) -> Self {
        Self { base_url, http }
    }

    /// Fetches the bootstrap blob for a thread. Caller is responsible for
    /// ensuring this is called at most once per thread per VM lifetime
    /// (the runtime's `OnceCell` guard handles that for the live path).
    pub async fn fetch_bootstrap(
        &self,
        thread_id: &str,
        tokens: &TokenRegistry,
    ) -> Result<ThreadBootstrap, GramClientError> {
        let url = format!("{}{}", self.base_url.trim_end_matches('/'), BOOTSTRAP_PATH);
        let bearer = tokens.current().map_err(|_| GramClientError::Token)?;

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

    pub async fn create_mcp_auth_flow(
        &self,
        thread_id: &str,
        server_id: &str,
        url: &str,
        tokens: &TokenRegistry,
    ) -> Result<CreateMcpAuthFlowResponse, GramClientError> {
        let endpoint = format!(
            "{}{}",
            self.base_url.trim_end_matches('/'),
            CREATE_MCP_AUTH_FLOW_PATH
        );
        let bearer = tokens.current().map_err(|_| GramClientError::Token)?;

        let resp = self
            .http
            .post(&endpoint)
            .timeout(BOOTSTRAP_TIMEOUT)
            .bearer_auth(&bearer)
            .json(&CreateMcpAuthFlowRequest {
                thread_id,
                server_id,
                url,
            })
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
        let flow: CreateMcpAuthFlowResponse = serde_json::from_str(&body)?;
        Ok(flow)
    }

    /// Persists a runner-produced compacted transcript as a new
    /// chat_messages generation so the next cold cron bootstrap loads the
    /// shorter history. The endpoint returns 204 on success; the body is
    /// ignored.
    pub async fn record_compacted_generation(
        &self,
        thread_id: &str,
        tokens: &TokenRegistry,
        messages: &[RunnerMessage],
    ) -> Result<(), GramClientError> {
        let endpoint = format!(
            "{}{}",
            self.base_url.trim_end_matches('/'),
            RECORD_COMPACTED_GENERATION_PATH
        );
        let bearer = tokens.current().map_err(|_| GramClientError::Token)?;

        let resp = self
            .http
            .post(&endpoint)
            .timeout(RECORD_COMPACTION_TIMEOUT)
            .bearer_auth(&bearer)
            .json(&RecordCompactedGenerationRequest {
                thread_id,
                messages,
            })
            .send()
            .await?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await?;
            return Err(GramClientError::Status {
                status: status.as_u16(),
                body,
            });
        }
        Ok(())
    }

    /// Asks the management API whether this tool call may execute. The
    /// server consults spend-gate and risk-policy enforcement. Callers must
    /// fail open on transport/decode errors so a control-plane blip does not
    /// wedge the assistant loop.
    pub async fn consult_tool_call(
        &self,
        thread_id: &str,
        tool_name: &str,
        tool_input: &serde_json::Value,
        tool_call_id: &str,
        tokens: &TokenRegistry,
    ) -> Result<ConsultToolCallResponse, GramClientError> {
        let url = format!(
            "{}{}",
            self.base_url.trim_end_matches('/'),
            CONSULT_TOOL_CALL_PATH
        );
        let bearer = tokens.current().map_err(|_| GramClientError::Token)?;

        let resp = self
            .http
            .post(&url)
            .timeout(CONSULT_TIMEOUT)
            .bearer_auth(&bearer)
            .json(&ConsultToolCallRequest {
                thread_id,
                tool_name,
                tool_input,
                tool_call_id,
            })
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
        let parsed: ConsultToolCallResponse = serde_json::from_str(&body)?;
        Ok(parsed)
    }
}
