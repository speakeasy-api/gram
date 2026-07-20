//! Tool-result image injection for the Slack inspect-file platform tool.
//!
//! The Go server's `platform_slack_inspect_file` tool downloads and validates
//! a Slack image, then returns it inside its JSON result under
//! [`INLINE_IMAGE_KEY`] as a `data:` URI. Tool messages cannot carry media —
//! the completions adapter rejects `Media` parts outside user items, and the
//! raw base64 would otherwise be clipped and burn tokens as text — so this
//! wrapper intercepts the result before it reaches the transcript: it strips
//! the image payload from the tool result (leaving metadata plus a note) and
//! forwards the image to the thread inbox as structured user content. The
//! run loop's existing `AfterToolResult` drain then submits it as a user item
//! ahead of the next model call, which `user_content_item` maps onto an
//! agentkit `Media` part.
//!
//! The wrapper sits directly around the MCP catalog (inside the compose
//! wrap), so both direct calls and compose-nested calls are intercepted, and
//! the byte-capping [`crate::clip::ClippedToolSource`] outside it only ever
//! sees the already-slimmed result.

use std::sync::Arc;

use agentkit_core::ToolOutput;
use agentkit_tools_core::{
    PermissionRequest, Tool, ToolCatalogEvent, ToolContext, ToolError, ToolName, ToolRequest,
    ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde_json::Value;
use tokio::sync::mpsc::UnboundedSender;

use crate::wire::{RunnerContent, RunnerContentPart, RunnerImageUrl};

/// Raw name of the Go platform tool whose results carry inline images. The
/// MCP catalog namespaces tool names as `mcp_<server>_<tool>`, so matching is
/// by suffix.
pub const INSPECT_FILE_TOOL: &str = "platform_slack_inspect_file";

/// Result field carrying the fetched image. Keep in sync with
/// `inlineImageResultKey` in
/// `server/internal/platformtools/slack/tool_inspect_file.go`.
const INLINE_IMAGE_KEY: &str = "gram_inline_image";

/// Wraps a [`ToolSource`] so results of the inspect-file tool have their
/// inline image extracted into `inbox_tx` as pending user content.
pub struct VisionInterceptSource<S> {
    inner: S,
    inbox_tx: UnboundedSender<RunnerContent>,
}

impl<S> VisionInterceptSource<S> {
    pub fn new(inner: S, inbox_tx: UnboundedSender<RunnerContent>) -> Self {
        Self { inner, inbox_tx }
    }
}

impl<S> ToolSource for VisionInterceptSource<S>
where
    S: ToolSource,
{
    fn specs(&self) -> Vec<ToolSpec> {
        self.inner.specs()
    }

    fn get(&self, name: &ToolName) -> Option<Arc<dyn Tool>> {
        let tool = self.inner.get(name)?;
        if name.0.ends_with(INSPECT_FILE_TOOL) {
            Some(Arc::new(VisionInterceptTool {
                inner: tool,
                inbox_tx: self.inbox_tx.clone(),
            }) as Arc<dyn Tool>)
        } else {
            Some(tool)
        }
    }

    fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
        self.inner.drain_catalog_events()
    }
}

struct VisionInterceptTool {
    inner: Arc<dyn Tool>,
    inbox_tx: UnboundedSender<RunnerContent>,
}

#[async_trait]
impl Tool for VisionInterceptTool {
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
        let tool_name = request.tool_name.clone();
        let call_id = request.call_id.clone();
        let mut result = self.inner.invoke(request, ctx).await?;
        result.result.output = extract_inline_image(
            result.result.output,
            tool_name.0.as_str(),
            &call_id.0,
            &self.inbox_tx,
        );
        Ok(result)
    }
}

/// Pulls the [`INLINE_IMAGE_KEY`] payload out of a tool result. When a valid
/// `data:image/...` URI is found, the image is queued on the inbox as user
/// content and the returned output is the original result minus the payload.
/// Results without the marker pass through untouched; a malformed marker is
/// stripped (never forwarded to the model) but injects nothing.
fn extract_inline_image(
    output: ToolOutput,
    tool_name: &str,
    call_id: &str,
    inbox_tx: &UnboundedSender<RunnerContent>,
) -> ToolOutput {
    let (mut value, was_text) = match &output {
        ToolOutput::Text(text) => match serde_json::from_str::<Value>(text) {
            Ok(value) => (value, true),
            Err(_) => return output,
        },
        ToolOutput::Structured(value) => (value.clone(), false),
        _ => return output,
    };
    let Some(object) = value.as_object_mut() else {
        return output;
    };
    let Some(inline) = object.remove(INLINE_IMAGE_KEY) else {
        return output;
    };

    let data_uri = inline
        .get("data_uri")
        .and_then(Value::as_str)
        .unwrap_or_default();
    if data_uri.starts_with("data:image/") {
        let file_id = inline
            .get("file_id")
            .and_then(Value::as_str)
            .unwrap_or("<unknown>");
        let text = format!(
            "Image from Slack file {file_id}, fetched by the {tool_name} tool call {call_id}. \
             It is attached below for inspection."
        );
        let content = RunnerContent::Parts(vec![
            RunnerContentPart::Text { text },
            RunnerContentPart::ImageUrl {
                image_url: RunnerImageUrl {
                    url: data_uri.to_string(),
                    detail: None,
                },
            },
        ]);
        if inbox_tx.send(content).is_ok() {
            object.insert("image_attached".to_string(), Value::Bool(true));
        } else {
            tracing::warn!(tool = %tool_name, "thread inbox closed; dropping inspected image");
        }
    } else {
        tracing::warn!(tool = %tool_name, "inline image marker without a data:image URI; stripping it");
    }

    if was_text {
        ToolOutput::Text(value.to_string())
    } else {
        ToolOutput::Structured(value)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use tokio::sync::mpsc;

    fn marker_result(data_uri: &str) -> String {
        format!(
            r#"{{"file":{{"id":"F123","name":"cat.png"}},"note":"image fetched","{INLINE_IMAGE_KEY}":{{"file_id":"F123","mime_type":"image/png","size_bytes":9,"data_uri":"{data_uri}"}}}}"#
        )
    }

    #[test]
    fn marker_result_strips_payload_and_queues_user_image() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let out = extract_inline_image(
            ToolOutput::Text(marker_result("data:image/png;base64,QUJD")),
            "mcp_srv_platform_slack_inspect_file",
            "call-1",
            &tx,
        );

        let ToolOutput::Text(text) = &out else {
            panic!("expected text output, got {out:?}");
        };
        let value: Value = serde_json::from_str(text).unwrap();
        assert!(
            value.get(INLINE_IMAGE_KEY).is_none(),
            "payload must be stripped"
        );
        assert_eq!(value["image_attached"], Value::Bool(true));
        assert_eq!(value["file"]["id"], "F123");

        let RunnerContent::Parts(parts) = rx.try_recv().unwrap() else {
            panic!("expected parts content");
        };
        assert_eq!(parts.len(), 2);
        let RunnerContentPart::Text { text } = &parts[0] else {
            panic!("expected leading text part");
        };
        assert!(text.contains("F123"));
        assert!(text.contains("call-1"));
        let RunnerContentPart::ImageUrl { image_url } = &parts[1] else {
            panic!("expected image part");
        };
        assert_eq!(image_url.url, "data:image/png;base64,QUJD");
    }

    #[test]
    fn markerless_result_passes_through_unchanged() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let out = extract_inline_image(
            ToolOutput::Text(r#"{"ok":true,"file":{"id":"F1"}}"#.to_string()),
            "mcp_srv_platform_slack_inspect_file",
            "call-1",
            &tx,
        );
        let ToolOutput::Text(text) = &out else {
            panic!("expected text output");
        };
        assert_eq!(text, r#"{"ok":true,"file":{"id":"F1"}}"#);
        assert!(rx.try_recv().is_err(), "nothing must be queued");
    }

    #[test]
    fn non_json_result_passes_through_unchanged() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let out = extract_inline_image(
            ToolOutput::Text("plain error text".to_string()),
            "tool",
            "call-1",
            &tx,
        );
        assert_eq!(out, ToolOutput::Text("plain error text".to_string()));
        assert!(rx.try_recv().is_err());
    }

    #[test]
    fn malformed_marker_is_stripped_without_injection() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let out = extract_inline_image(
            ToolOutput::Text(marker_result("https://evil.example.com/not-a-data-uri")),
            "tool",
            "call-1",
            &tx,
        );
        let ToolOutput::Text(text) = &out else {
            panic!("expected text output");
        };
        let value: Value = serde_json::from_str(text).unwrap();
        assert!(
            value.get(INLINE_IMAGE_KEY).is_none(),
            "marker must not reach the model"
        );
        assert!(value.get("image_attached").is_none());
        assert!(rx.try_recv().is_err(), "non-data URIs must not inject");
    }

    #[test]
    fn structured_output_is_also_intercepted() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let structured: Value =
            serde_json::from_str(&marker_result("data:image/jpeg;base64,QQ==")).unwrap();
        let out = extract_inline_image(ToolOutput::Structured(structured), "tool", "c", &tx);
        let ToolOutput::Structured(value) = &out else {
            panic!("expected structured output");
        };
        assert!(value.get(INLINE_IMAGE_KEY).is_none());
        assert_eq!(value["image_attached"], Value::Bool(true));
        assert!(rx.try_recv().is_ok());
    }
}
