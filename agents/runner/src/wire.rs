use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::compaction::CompactionPolicy;

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
/// optional `mcp_servers` reconciles the assistant-wide MCP set so
/// toolset edits made after bootstrap take effect without recycling the
/// VM.
#[derive(Debug, Deserialize)]
pub struct ThreadTurnRequest {
    pub input: String,
    /// Optional structured content parts appended after `input` when the
    /// turn's user item is built, so a turn can attach images without
    /// changing the string-typed `input` contract. Mirrors
    /// `runtimeTurnRequest.InputParts` on the Go side.
    #[serde(default)]
    pub input_parts: Option<Vec<RunnerContentPart>>,
    #[serde(default)]
    pub auth_token: Option<String>,
    #[serde(default)]
    pub mcp_servers: Option<Vec<McpServer>>,
    /// Identity for a runner that booted without `GRAM_ASSISTANT_ID` — i.e. a
    /// generic warm-pool sandbox that learns which assistant it serves from
    /// the first turn. Ignored once the boot env has set it (env wins).
    #[serde(default)]
    pub assistant_id: Option<String>,
    /// Project the assistant belongs to. Set-once like `assistant_id`; only
    /// used to stamp exported trace spans so traces filter per project.
    #[serde(default)]
    pub project_id: Option<String>,
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
    pub content: RunnerContent,
    #[serde(default)]
    pub tool_calls: Vec<RunnerToolCall>,
    #[serde(default)]
    pub tool_call_id: Option<String>,
}

/// String-or-parts content union carried in a message's content slot,
/// mirroring `runtimeContent` on the Go side (and the OpenRouter content
/// union). Untagged: a bare JSON string decodes as `Text`, an array as
/// `Parts`, and `Text` serializes back to a bare string so servers that
/// predate structured parts keep interoperating.
#[derive(Debug, Deserialize, Serialize, Clone, Hash, PartialEq, Eq)]
#[serde(untagged)]
pub enum RunnerContent {
    Text(String),
    Parts(Vec<RunnerContentPart>),
}

impl Default for RunnerContent {
    fn default() -> Self {
        RunnerContent::Text(String::new())
    }
}

impl RunnerContent {
    /// Folds a turn's plain `input` and optional `input_parts` into one
    /// content value: parts-less turns stay plain text, and a non-empty
    /// `input` becomes a text part ahead of the structured parts.
    pub fn from_turn(input: String, input_parts: Option<Vec<RunnerContentPart>>) -> Self {
        match input_parts {
            None => RunnerContent::Text(input),
            Some(parts) if parts.is_empty() => RunnerContent::Text(input),
            Some(parts) => {
                let mut all = Vec::with_capacity(parts.len() + 1);
                if !input.is_empty() {
                    all.push(RunnerContentPart::Text { text: input });
                }
                all.extend(parts);
                RunnerContent::Parts(all)
            }
        }
    }
}

/// One element of a structured content array, in the OpenAI-style
/// `{"type": ...}` shape the OpenRouter wire uses.
#[derive(Debug, Deserialize, Serialize, Clone, Hash, PartialEq, Eq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum RunnerContentPart {
    Text { text: String },
    ImageUrl { image_url: RunnerImageUrl },
}

#[derive(Debug, Deserialize, Serialize, Clone, Hash, PartialEq, Eq)]
pub struct RunnerImageUrl {
    pub url: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
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
    #[serde(default)]
    pub compaction: CompactionPolicy,
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;

    #[test]
    fn content_bare_string_round_trips() {
        let msg: RunnerMessage = serde_json::from_str(r#"{"role":"user","content":"hi"}"#).unwrap();
        assert_eq!(msg.content, RunnerContent::Text("hi".to_string()));
        let out = serde_json::to_string(&msg).unwrap();
        assert!(
            out.contains(r#""content":"hi""#),
            "text content must re-encode as a bare string, got {out}"
        );
    }

    #[test]
    fn content_defaults_to_empty_text_when_missing() {
        let msg: RunnerMessage = serde_json::from_str(r#"{"role":"assistant"}"#).unwrap();
        assert_eq!(msg.content, RunnerContent::Text(String::new()));
    }

    #[test]
    fn content_parts_round_trip() {
        let raw = r#"{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"high"}}]}"#;
        let msg: RunnerMessage = serde_json::from_str(raw).unwrap();
        let RunnerContent::Parts(parts) = &msg.content else {
            panic!("expected parts, got {:?}", msg.content);
        };
        assert_eq!(parts.len(), 2);
        assert_eq!(
            parts[1],
            RunnerContentPart::ImageUrl {
                image_url: RunnerImageUrl {
                    url: "https://example.com/a.png".to_string(),
                    detail: Some("high".to_string()),
                },
            }
        );

        let out = serde_json::to_string(&msg).unwrap();
        let back: RunnerMessage = serde_json::from_str(&out).unwrap();
        assert_eq!(back.content, msg.content);
    }

    #[test]
    fn image_url_detail_omitted_when_absent() {
        let part = RunnerContentPart::ImageUrl {
            image_url: RunnerImageUrl {
                url: "https://example.com/a.png".to_string(),
                detail: None,
            },
        };
        let out = serde_json::to_string(&part).unwrap();
        assert_eq!(
            out,
            r#"{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}"#
        );
    }

    #[test]
    fn from_turn_without_parts_stays_text() {
        assert_eq!(
            RunnerContent::from_turn("hello".to_string(), None),
            RunnerContent::Text("hello".to_string())
        );
        assert_eq!(
            RunnerContent::from_turn("hello".to_string(), Some(Vec::new())),
            RunnerContent::Text("hello".to_string())
        );
    }

    #[test]
    fn from_turn_prepends_input_as_text_part() {
        let parts = vec![RunnerContentPart::ImageUrl {
            image_url: RunnerImageUrl {
                url: "https://example.com/a.png".to_string(),
                detail: None,
            },
        }];
        let content = RunnerContent::from_turn("caption".to_string(), Some(parts.clone()));
        let RunnerContent::Parts(all) = content else {
            panic!("expected parts");
        };
        assert_eq!(all.len(), 2);
        assert_eq!(
            all[0],
            RunnerContentPart::Text {
                text: "caption".to_string()
            }
        );
        assert_eq!(all[1], parts[0]);
    }

    #[test]
    fn from_turn_empty_input_yields_parts_only() {
        let parts = vec![RunnerContentPart::Text {
            text: "just a part".to_string(),
        }];
        assert_eq!(
            RunnerContent::from_turn(String::new(), Some(parts.clone())),
            RunnerContent::Parts(parts)
        );
    }
}
