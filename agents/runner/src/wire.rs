use std::collections::BTreeMap;

use agentkit_core::{Item, Usage};
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct RunnerConfig {
    pub model: String,
    pub instructions: Option<String>,
    pub auth_token: String,
    pub completions_url: Option<String>,
    pub chat_id: String,
    #[serde(default)]
    pub mcp_servers: Vec<McpServer>,
    /// Prior transcript to prime the driver with at configure time. The loop
    /// comes up already hydrated; /turn carries only new user input after that.
    #[serde(default)]
    pub history: Vec<RunnerMessage>,
}

#[derive(Debug, Deserialize, Serialize, Clone, Hash)]
pub struct McpServer {
    pub id: String,
    pub url: String,
    #[serde(default)]
    pub headers: BTreeMap<String, String>,
}

#[derive(Debug, Deserialize)]
pub struct RunnerRequest {
    pub input: String,
    #[serde(default)]
    pub auth_token: Option<String>,
}

// RunnerMessage is the wire shape used to rehydrate transcript items at
// /configure. It mirrors server/internal/assistants/runtime.go's
// runtimeMessage one field at a time — keep them in sync.
#[derive(Debug, Deserialize, Serialize, Clone, Hash)]
pub struct RunnerMessage {
    pub role: String,
    #[serde(default)]
    pub content: String,
    #[serde(default)]
    pub tool_calls: Vec<RunnerToolCall>,
    #[serde(default)]
    pub tool_call_id: Option<String>,
}

#[derive(Debug, Deserialize, Serialize, Clone, Hash)]
pub struct RunnerToolCall {
    pub id: String,
    pub name: String,
    // JSON-encoded string matching the OpenAI tool-call arguments shape; stored
    // verbatim in the DB so the bytes we persist equal the bytes we replay.
    pub arguments: String,
}

#[derive(Debug, Serialize)]
pub struct RunnerResponse {
    pub finish_reason: String,
    pub final_text: String,
    pub items: Vec<Item>,
    pub usage: Option<Usage>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

impl RunnerResponse {
    pub fn deduped() -> Self {
        Self {
            finish_reason: "deduped".to_string(),
            final_text: String::new(),
            items: Vec::new(),
            usage: None,
            error: None,
        }
    }

    pub fn accepted() -> Self {
        Self {
            finish_reason: "accepted".to_string(),
            final_text: String::new(),
            items: Vec::new(),
            usage: None,
            error: None,
        }
    }
}

#[derive(Debug, Serialize)]
pub struct RunnerStateResponse {
    pub configured: bool,
    /// Seconds the loop has been idle (between turns). `0` while a turn is in
    /// flight. Backend reapers read this to decide TTL eviction. Absent when
    /// the runner has never been /configured.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idle_seconds: Option<u64>,
}
