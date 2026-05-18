use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize, Clone, Hash, PartialEq, Eq)]
pub struct McpServer {
    pub id: String,
    pub url: String,
    #[serde(default)]
    pub headers: BTreeMap<String, String>,
}

/// `/threads/{thread_id}/turn` request body. The runner looks up — or
/// bootstraps — a per-thread tokio task on first hit and enqueues `input`
/// onto its inbox. `auth_token` rotates the host's shared bearer; an
/// optional `mcp_servers` reconciles the assistant-wide MCP set.
#[derive(Debug, Deserialize)]
pub struct ThreadTurnRequest {
    pub input: String,
    #[serde(default)]
    pub auth_token: Option<String>,
}

/// 202-style ack returned by `/threads/{thread_id}/turn`. The actual turn
/// runs asynchronously on the per-thread tokio task; outputs land on
/// `/chat/completions` via the host's bearer.
#[derive(Debug, Serialize)]
pub struct ThreadTurnResponse {
    pub finish_reason: String,
}

impl ThreadTurnResponse {
    pub fn deduped() -> Self {
        Self {
            finish_reason: "deduped".to_string(),
        }
    }

    pub fn accepted() -> Self {
        Self {
            finish_reason: "accepted".to_string(),
        }
    }
}

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
    pub arguments: String,
}

/// `/state` reply. The backend reaper polls this to evict idle threads
/// and to confirm the VM still belongs to the assistant it admitted.
#[derive(Debug, Serialize)]
pub struct RunnerStateResponse {
    pub assistant_id: String,
    pub uptime_seconds: u64,
    pub threads: Vec<ThreadStateView>,
}

#[derive(Debug, Serialize)]
pub struct ThreadStateView {
    pub thread_id: String,
    pub chat_id: String,
    pub idle_seconds: u64,
}

/// Bootstrap blob the runner pulls from
/// `POST /rpc/assistants.getThreadBootstrap` on the first /turn for a
/// thread. Mirrors `server/internal/assistants/runtime.go::threadBootstrap`.
#[derive(Debug, Deserialize, Clone)]
pub struct ThreadBootstrap {
    pub model: String,
    #[serde(default)]
    pub instructions: String,
    pub completions_url: String,
    pub chat_id: String,
    #[serde(default)]
    pub mcp_servers: Vec<McpServer>,
    #[serde(default)]
    pub history: Vec<RunnerMessage>,
    #[serde(default)]
    pub context_window: Option<u64>,
}
