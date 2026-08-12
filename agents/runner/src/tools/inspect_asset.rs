//! Generic asset inspection: fetch an image by URL and attach it to the
//! conversation as user content.
//!
//! Tool messages cannot carry media — the completions adapter rejects
//! `Media` parts outside user items — so the fetched image is forwarded to
//! the thread inbox as structured user content. The run loop's
//! `AfterToolResult` drain submits it as a user item ahead of the next model
//! call, and the tool result itself carries only metadata.
//!
//! Providers whose downloads need credentials (e.g. Slack private files)
//! integrate by exposing a tool that mints a short-lived, directly fetchable
//! URL; the model then passes that URL here.

use std::time::Duration;

use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    Tool, ToolAnnotations, ToolContext, ToolError, ToolRequest, ToolResult, ToolSpec,
};
use async_trait::async_trait;
use base64::Engine as _;
use serde::Deserialize;
use serde_json::json;
use tokio::sync::mpsc::UnboundedSender;

use crate::wire::{RunnerContent, RunnerContentPart, RunnerImageUrl};

const TOOL_NAME: &str = "inspect_asset";

/// Caps a fetched asset at 10 MiB, mirroring the Go platform fetcher's bound.
const MAX_ASSET_BYTES: usize = 10 * 1024 * 1024;

const FETCH_TIMEOUT: Duration = Duration::from_secs(30);

pub struct InspectAssetTool {
    http: reqwest::Client,
    inbox_tx: UnboundedSender<RunnerContent>,
    spec: ToolSpec,
}

impl InspectAssetTool {
    pub fn new(inbox_tx: UnboundedSender<RunnerContent>) -> Self {
        let http = reqwest::Client::builder()
            .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
            .timeout(FETCH_TIMEOUT)
            .build()
            .unwrap_or_default();
        Self {
            http,
            inbox_tx,
            spec: build_spec(),
        }
    }
}

fn build_spec() -> ToolSpec {
    let input_schema = json!({
        "type": "object",
        "properties": {
            "url": {
                "type": "string",
                "description": "http(s) URL of the image to fetch and inspect.",
            },
        },
        "required": ["url"],
        "additionalProperties": false,
    });

    let description = "Fetch an image by URL (png, jpeg, gif, or webp, up to 10 MiB) and \
attach it to the conversation so you can see it. Works with any directly fetchable http(s) \
URL, including short-lived download URLs returned by other tools. The tool result carries \
the fetch metadata; the image itself arrives as a follow-up user message.";

    ToolSpec::new(TOOL_NAME, description, input_schema)
        .with_annotations(ToolAnnotations::default().with_read_only(true))
}

#[derive(Debug, Deserialize)]
struct InspectAssetInput {
    url: String,
}

#[async_trait]
impl Tool for InspectAssetTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        _ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        let call_id = request.call_id.clone();
        let input: InspectAssetInput = serde_json::from_value(request.input)
            .map_err(|e| ToolError::InvalidInput(e.to_string()))?;

        let (text, is_error) = match self.fetch(&input.url).await {
            Ok((data, mime)) => {
                match attach_image(&data, mime, &input.url, &call_id.0, &self.inbox_tx) {
                    Ok(()) => (
                        json!({
                            "url": input.url,
                            "mime_type": mime,
                            "size_bytes": data.len(),
                            "image_attached": true,
                            "note": "the image is attached to the conversation as a user message",
                        })
                        .to_string(),
                        false,
                    ),
                    Err(e) => (e, true),
                }
            }
            Err(e) => (format!("fetch asset: {e}"), true),
        };

        Ok(ToolResult {
            result: ToolResultPart {
                call_id,
                output: ToolOutput::text(text),
                is_error,
                metadata: MetadataMap::new(),
            },
            duration: None,
            metadata: MetadataMap::new(),
        })
    }
}

impl InspectAssetTool {
    async fn fetch(&self, url: &str) -> Result<(Vec<u8>, &'static str), String> {
        if !url.starts_with("https://") && !url.starts_with("http://") {
            return Err(format!("url must be http(s), got {url:?}"));
        }
        let resp = self.http.get(url).send().await.map_err(|e| e.to_string())?;
        let status = resp.status();
        if !status.is_success() {
            return Err(format!("request returned {status}"));
        }
        if resp.content_length().unwrap_or(0) > MAX_ASSET_BYTES as u64 {
            return Err(format!("asset exceeds the {MAX_ASSET_BYTES} byte limit"));
        }
        let data = resp.bytes().await.map_err(|e| e.to_string())?;
        if data.len() > MAX_ASSET_BYTES {
            return Err(format!("asset exceeds the {MAX_ASSET_BYTES} byte limit"));
        }
        if data.is_empty() {
            return Err("asset is empty".to_string());
        }
        let mime = sniff_image_mime(&data)
            .ok_or_else(|| "content is not a supported image (png, jpeg, gif, webp)".to_string())?;
        Ok((data.to_vec(), mime))
    }
}

/// Queues the fetched image on the thread inbox as user content: a text part
/// naming the source, then the image as a `data:` URI part.
fn attach_image(
    data: &[u8],
    mime: &str,
    url: &str,
    call_id: &str,
    inbox_tx: &UnboundedSender<RunnerContent>,
) -> Result<(), String> {
    let data_uri = format!(
        "data:{mime};base64,{}",
        base64::engine::general_purpose::STANDARD.encode(data)
    );
    let text = format!(
        "Image fetched from {url} by the {TOOL_NAME} tool call {call_id}. It is attached below for inspection."
    );
    let content = RunnerContent::Parts(vec![
        RunnerContentPart::Text { text },
        RunnerContentPart::ImageUrl {
            image_url: RunnerImageUrl {
                url: data_uri,
                detail: None,
            },
        },
    ]);
    inbox_tx
        .send(content)
        .map_err(|_| "thread inbox closed; image could not be attached".to_string())
}

/// Magic-byte sniff against the image allowlist. The response Content-Type
/// header is advisory only; the bytes decide.
fn sniff_image_mime(data: &[u8]) -> Option<&'static str> {
    if data.starts_with(b"\x89PNG\r\n\x1a\n") {
        Some("image/png")
    } else if data.starts_with(&[0xFF, 0xD8, 0xFF]) {
        Some("image/jpeg")
    } else if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
        Some("image/gif")
    } else if data.len() >= 12 && data.starts_with(b"RIFF") && &data[8..12] == b"WEBP" {
        Some("image/webp")
    } else {
        None
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use tokio::sync::mpsc;

    #[test]
    fn sniffs_supported_image_formats() {
        assert_eq!(
            sniff_image_mime(b"\x89PNG\r\n\x1a\n0000"),
            Some("image/png")
        );
        assert_eq!(
            sniff_image_mime(&[0xFF, 0xD8, 0xFF, 0xE0]),
            Some("image/jpeg")
        );
        assert_eq!(sniff_image_mime(b"GIF89a0000"), Some("image/gif"));
        assert_eq!(
            sniff_image_mime(b"RIFF\0\0\0\0WEBP0000"),
            Some("image/webp")
        );
        assert_eq!(sniff_image_mime(b"<html><body>nope</body></html>"), None);
        assert_eq!(sniff_image_mime(b""), None);
    }

    #[test]
    fn attach_image_queues_text_and_data_uri_parts() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        attach_image(
            b"ABC",
            "image/png",
            "https://example.com/a.png",
            "call-1",
            &tx,
        )
        .unwrap();

        let RunnerContent::Parts(parts) = rx.try_recv().unwrap() else {
            panic!("expected parts content");
        };
        assert_eq!(parts.len(), 2);
        let RunnerContentPart::Text { text } = &parts[0] else {
            panic!("expected leading text part");
        };
        assert!(text.contains("https://example.com/a.png"));
        assert!(text.contains("call-1"));
        let RunnerContentPart::ImageUrl { image_url } = &parts[1] else {
            panic!("expected image part");
        };
        assert_eq!(image_url.url, "data:image/png;base64,QUJD");
    }

    #[test]
    fn attach_image_reports_closed_inbox() {
        let (tx, rx) = mpsc::unbounded_channel();
        drop(rx);
        let err = attach_image(b"ABC", "image/png", "u", "c", &tx).unwrap_err();
        assert!(err.contains("inbox closed"));
    }
}
