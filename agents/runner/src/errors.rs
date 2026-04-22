use std::time::Duration;

use thiserror::Error;

#[derive(Debug, Error)]
pub enum RunnerError {
    #[error("invalid mcp server header name for {server}: {name}: {source}")]
    McpHeaderName {
        server: String,
        name: String,
        #[source]
        source: http::header::InvalidHeaderName,
    },

    #[error("invalid mcp server header value for {server}/{name}: {source}")]
    McpHeaderValue {
        server: String,
        name: String,
        #[source]
        source: http::header::InvalidHeaderValue,
    },

    #[error("invalid header value: {source}")]
    HeaderValue {
        #[source]
        source: http::header::InvalidHeaderValue,
    },

    #[error("http client build failed: {0}")]
    HttpClient(#[from] reqwest::Error),

    #[error("agent build failed: {0}")]
    AgentBuild(String),

    #[error("mcp connect failed: {0}")]
    McpConnect(String),

    #[error("mcp connect timed out after {0:?}")]
    McpConnectTimeout(Duration),

    #[error("agent session start failed: {0}")]
    AgentStart(String),

    #[error("agent session start timed out after {0:?}")]
    AgentStartTimeout(Duration),

    #[error("loop error: {0}")]
    Loop(String),

    #[error("unexpected mcp auth interrupt — token likely expired or backend returned 403")]
    McpAuthInterrupt,

    #[error("unsupported history role: {0}")]
    UnsupportedHistoryRole(String),

    #[error("tool history message missing tool_call_id")]
    MissingToolCallId,

    #[error("decode tool_call arguments for id {id}: {source}")]
    ToolCallArguments {
        id: String,
        #[source]
        source: serde_json::Error,
    },

    #[error("submit input: {0}")]
    SubmitInput(String),
}

impl From<agentkit_loop::LoopError> for RunnerError {
    fn from(err: agentkit_loop::LoopError) -> Self {
        RunnerError::Loop(err.to_string())
    }
}
