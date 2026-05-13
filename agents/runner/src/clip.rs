//! Per-tool-result byte clipper for the assistant runtime.
//!
//! OpenRouter caps a single tool message at 204800 bytes and 413s the next
//! request when a tool result exceeds it. Token-aware compaction can't catch
//! this: the trigger reads `usage.input_tokens` after the previous turn, and
//! 200 KB ≈ 50 K tokens — well below the 80 % threshold for a 128 K+ window.
//! Even when it does fire, [`SummarizeOlderStrategy`](agentkit_compaction)
//! preserves the most recent items, so the offending tool result survives.
//!
//! [`ClippedToolSource`] wraps a [`ToolSource`] (the MCP catalog reader) and
//! truncates each [`ToolOutput`] to [`MAX_TOOL_BYTES`] before it leaves the
//! tool. Native tools (`bun_run`) already clip themselves and don't need
//! wrapping — the bun limits are smaller, so they win.

use std::sync::Arc;

use agentkit_core::ToolOutput;
use agentkit_tools_core::{
    PermissionRequest, Tool, ToolCatalogEvent, ToolContext, ToolError, ToolName, ToolRequest,
    ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde_json::json;

/// 150 KB. Leaves ~50 KB headroom under OpenRouter's 204800-byte per-message
/// cap to absorb provider-side JSON envelope overhead (call id, metadata,
/// content-type discriminators).
pub const MAX_TOOL_BYTES: usize = 150_000;

/// Wraps a [`ToolSource`] so every tool it serves clips its output to
/// [`MAX_TOOL_BYTES`].
pub struct ClippedToolSource<S> {
    inner: S,
}

impl<S> ClippedToolSource<S> {
    pub fn new(inner: S) -> Self {
        Self { inner }
    }
}

impl<S> ToolSource for ClippedToolSource<S>
where
    S: ToolSource,
{
    fn specs(&self) -> Vec<ToolSpec> {
        self.inner.specs()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        self.inner
            .get(name)
            .map(|inner| Arc::new(ClippedTool { inner }) as Arc<dyn Tool>)
    }

    fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
        self.inner.drain_catalog_events()
    }
}

struct ClippedTool {
    inner: Arc<dyn Tool>,
}

#[async_trait]
impl Tool for ClippedTool {
    fn spec(&self) -> &ToolSpec {
        self.inner.spec()
    }

    fn current_spec(&self) -> Option<ToolSpec> {
        self.inner.current_spec()
    }

    fn proposed_requests(
        &self,
        request: &ToolRequest,
    ) -> Result<Vec<Box<dyn PermissionRequest>>, ToolError> {
        self.inner.proposed_requests(request)
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        let mut result = self.inner.invoke(request, ctx).await?;
        result.result.output = clip_output(result.result.output, MAX_TOOL_BYTES);
        Ok(result)
    }
}

fn clip_output(output: ToolOutput, max_bytes: usize) -> ToolOutput {
    if let ToolOutput::Text(s) = output {
        if s.len() <= max_bytes {
            return ToolOutput::Text(s);
        }
        let cut = s.floor_char_boundary(max_bytes);
        let dropped = s.len() - cut;
        return ToolOutput::Text(format!("{}…[truncated {dropped} bytes]", &s[..cut]));
    }
    let Ok(serialized) = serde_json::to_string(&output) else {
        return output;
    };
    if serialized.len() <= max_bytes {
        return output;
    }
    let original_bytes = serialized.len();
    let cut = serialized.floor_char_boundary(max_bytes);
    ToolOutput::Structured(json!({
        "truncated": true,
        "original_bytes": original_bytes,
        "kept_bytes": cut,
        "content": &serialized[..cut],
    }))
}

#[cfg(test)]
mod tests {
    use super::*;
    use agentkit_core::{Part, TextPart};
    use serde_json::{Value, json};

    #[test]
    fn text_under_limit_passes_through() {
        let out = clip_output(ToolOutput::text("hello"), 100);
        match out {
            ToolOutput::Text(s) => assert_eq!(s, "hello"),
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[test]
    fn text_at_exact_limit_passes_through() {
        let body = "a".repeat(100);
        let out = clip_output(ToolOutput::text(body.clone()), 100);
        match out {
            ToolOutput::Text(s) => assert_eq!(s, body),
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[test]
    fn text_over_limit_is_clipped_with_marker() {
        let body = "a".repeat(200);
        let out = clip_output(ToolOutput::text(body), 100);
        match out {
            ToolOutput::Text(s) => {
                assert!(s.starts_with(&"a".repeat(100)), "kept prefix");
                assert!(s.contains("truncated 100 bytes"), "marker present: {s}");
            }
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[test]
    fn text_clip_respects_utf8_boundary() {
        // 50 × "é" = 100 bytes total. Cut at byte 75 sits inside a 2-byte
        // codepoint; floor_char_boundary must walk back to 74.
        let body = "é".repeat(50);
        let out = clip_output(ToolOutput::text(body), 75);
        match out {
            ToolOutput::Text(s) => {
                let prefix = s.split('…').next().unwrap();
                assert!(prefix.chars().all(|c| c == 'é'));
                assert!(prefix.len() <= 75);
            }
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[test]
    fn structured_under_limit_passes_through() {
        let value = json!({"k": "v"});
        let out = clip_output(ToolOutput::Structured(value.clone()), 1000);
        match out {
            ToolOutput::Structured(got) => assert_eq!(got, value),
            other => panic!("expected Structured, got {other:?}"),
        }
    }

    #[test]
    fn structured_over_limit_is_wrapped() {
        let big = "x".repeat(500);
        let value = json!({"payload": big});
        let input = ToolOutput::Structured(value);
        let serialized_len = serde_json::to_string(&input).unwrap().len();
        let out = clip_output(input, 200);
        match out {
            ToolOutput::Structured(Value::Object(map)) => {
                assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
                let original = map
                    .get("original_bytes")
                    .and_then(Value::as_u64)
                    .expect("original_bytes");
                let kept = map.get("kept_bytes").and_then(Value::as_u64).expect("kept");
                assert_eq!(original as usize, serialized_len);
                assert!(kept <= 200);
                assert!(map.get("content").and_then(Value::as_str).is_some());
            }
            other => panic!("expected Structured object, got {other:?}"),
        }
    }

    #[test]
    fn parts_over_limit_falls_back_to_truncation_envelope() {
        let big = "x".repeat(500);
        let parts = vec![Part::Text(TextPart::new(big))];
        let out = clip_output(ToolOutput::Parts(parts), 100);
        match out {
            ToolOutput::Structured(Value::Object(map)) => {
                assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
                assert!(map.get("original_bytes").is_some());
            }
            other => panic!("expected truncation envelope, got {other:?}"),
        }
    }
}
