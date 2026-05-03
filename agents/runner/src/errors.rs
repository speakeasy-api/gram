use http::StatusCode;
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

    #[error("agent session start failed: {0}")]
    AgentStart(String),

    #[error("loop error: {0}")]
    Loop(String),

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

    #[error("config error for key: {key}")]
    ConfigError { key: String },
}

impl From<agentkit_loop::LoopError> for RunnerError {
    fn from(err: agentkit_loop::LoopError) -> Self {
        RunnerError::Loop(err.to_string())
    }
}

impl RunnerError {
    pub fn configure_status_code(&self) -> StatusCode {
        match self {
            RunnerError::AgentStart(_) | RunnerError::HttpClient(_) => {
                StatusCode::SERVICE_UNAVAILABLE
            }
            RunnerError::McpHeaderName { .. }
            | RunnerError::McpHeaderValue { .. }
            | RunnerError::HeaderValue { .. }
            | RunnerError::UnsupportedHistoryRole(_)
            | RunnerError::MissingToolCallId
            | RunnerError::ToolCallArguments { .. }
            | RunnerError::ConfigError { .. } => StatusCode::BAD_REQUEST,
            RunnerError::AgentBuild(_) | RunnerError::Loop(_) | RunnerError::SubmitInput(_) => {
                StatusCode::INTERNAL_SERVER_ERROR
            }
        }
    }
}
