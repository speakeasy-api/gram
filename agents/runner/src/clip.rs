//! Per-tool-result byte cap for the assistant runtime.
//!
//! OpenRouter caps a single tool message at 204800 bytes and 413s the next
//! request when a tool result exceeds it. Token-aware compaction can't catch
//! this: the trigger reads `usage.input_tokens` after the previous turn, and
//! 200 KB ≈ 50 K tokens — well below the 80 % threshold for a 128 K+ window.
//! Even when it does fire, [`SummarizeOlderStrategy`](agentkit_compaction)
//! preserves the most recent items, so the offending tool result survives.
//!
//! When a tool result exceeds [`MAX_TOOL_BYTES`], the runner replaces it
//! with a Structured envelope that always carries a truncated `preview` of
//! the body (sized so the envelope itself fits under the cap). It also
//! best-effort spills the leading bytes of the body to a file inside the
//! assistant workdir so the agent can read or grep the full prefix via
//! filesystem tools in the same session; the envelope surfaces that path
//! via `saved_to` when the spill succeeds. The inline preview is what
//! makes the envelope resumable: each runtime boot copies a fresh workdir,
//! so any `saved_to` path stored in chat history won't exist after a
//! restart — but the preview is captured in the transcript and replays
//! intact.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use agentkit_core::{ToolCallId, ToolOutput};
use agentkit_tools_core::{
    PermissionRequest, Tool, ToolCatalogEvent, ToolContext, ToolError, ToolName, ToolRequest,
    ToolResult, ToolSource, ToolSpec,
};
use async_trait::async_trait;
use serde_json::Value;

/// 150 KB. Leaves ~50 KB headroom under OpenRouter's 204800-byte per-message
/// cap to absorb provider-side JSON envelope overhead (call id, metadata,
/// content-type discriminators).
pub const MAX_TOOL_BYTES: usize = 150_000;

/// 4 MiB. The assistant workdir is a fixed 256 MiB ext4 image
/// (`agents/runtime-image/Dockerfile`), shared with `bun_run` temp files and
/// any artifacts the agent writes itself. Bounding each spill keeps a single
/// runaway MCP response from filling the image and breaking subsequent
/// filesystem-tool / `bun_run` / spill calls. Truncated spills still carry
/// the leading body — enough for the agent to inspect with `head`/`grep`.
pub const MAX_SPILL_BYTES: usize = 4 * 1024 * 1024;

/// Wraps a [`ToolSource`] so every tool result either fits under
/// [`MAX_TOOL_BYTES`] or is spilled to a file under `spill_root` and replaced
/// with a small in-band pointer.
pub struct ClippedToolSource<S> {
    inner: S,
    spill_root: PathBuf,
}

impl<S> ClippedToolSource<S> {
    pub fn new(inner: S, spill_root: impl Into<PathBuf>) -> Self {
        Self {
            inner,
            spill_root: spill_root.into(),
        }
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
        self.inner.get(name).map(|inner| {
            Arc::new(ClippedTool {
                inner,
                spill_root: self.spill_root.clone(),
            }) as Arc<dyn Tool>
        })
    }

    fn drain_catalog_events(&self) -> Vec<ToolCatalogEvent> {
        self.inner.drain_catalog_events()
    }
}

struct ClippedTool {
    inner: Arc<dyn Tool>,
    spill_root: PathBuf,
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
        let tool_name = request.tool_name.clone();
        let call_id = request.call_id.clone();
        let mut result = self.inner.invoke(request, ctx).await?;
        result.result.output = cap_output(
            result.result.output,
            MAX_TOOL_BYTES,
            &self.spill_root,
            &tool_name,
            &call_id,
        )
        .await;
        Ok(result)
    }
}

async fn cap_output(
    output: ToolOutput,
    max_bytes: usize,
    spill_root: &Path,
    tool_name: &ToolName,
    call_id: &ToolCallId,
) -> ToolOutput {
    let Some(model_bytes) = model_bytes(&output) else {
        return output;
    };
    if model_bytes <= max_bytes {
        return output;
    }
    let (body, ext) = consume_for_spill(output);
    let spill = match write_spill(spill_root, tool_name, call_id, &body, ext, MAX_SPILL_BYTES).await
    {
        Ok(info) => Some(info),
        Err(error) => {
            tracing::warn!(
                error = %error,
                spill_root = %spill_root.display(),
                "spill of oversized tool result failed; emitting inline preview only",
            );
            None
        }
    };
    build_truncation_envelope(&body, ext, max_bytes, model_bytes, spill.as_ref())
}

/// JSON-encoded byte size of the *content* string that
/// `agentkit_adapter_completions` will ship as the tool message's `content`
/// field on the wire. The adapter flattens every variant into a single
/// `String` (text as-is, JSON variants via `value.to_string()` /
/// `serde_json::to_string(parts/files)`) and then puts that string in the
/// message envelope, where it gets JSON-encoded again. Measuring against
/// the doubly-escaped form is what catches non-Text overflow — compact JSON
/// full of `"` is ~2× when re-encoded as a string literal. Returns `None`
/// only if serialization fails; the cap can't be enforced and the output
/// is returned untouched.
fn model_bytes(output: &ToolOutput) -> Option<usize> {
    let content = adapter_content(output)?;
    serde_json::to_string(&content).ok().map(|s| s.len())
}

fn adapter_content(output: &ToolOutput) -> Option<String> {
    match output {
        ToolOutput::Text(s) => Some(s.clone()),
        ToolOutput::Structured(v) => Some(v.to_string()),
        ToolOutput::Parts(p) => serde_json::to_string(p).ok(),
        ToolOutput::Files(f) => serde_json::to_string(f).ok(),
    }
}

/// Converts the output into the body that will be written to disk. JSON
/// variants are pretty-printed so the spilled file is easier for the agent
/// to read or grep with the filesystem tools.
fn consume_for_spill(output: ToolOutput) -> (String, &'static str) {
    match output {
        ToolOutput::Text(s) => (s, "txt"),
        ToolOutput::Structured(v) => (serde_json::to_string_pretty(&v).unwrap_or_default(), "json"),
        ToolOutput::Parts(p) => (serde_json::to_string_pretty(&p).unwrap_or_default(), "json"),
        ToolOutput::Files(f) => (serde_json::to_string_pretty(&f).unwrap_or_default(), "json"),
    }
}

async fn write_spill(
    spill_root: &Path,
    tool_name: &ToolName,
    call_id: &ToolCallId,
    body: &str,
    ext: &str,
    max_spill_bytes: usize,
) -> std::io::Result<(PathBuf, usize)> {
    // Belt-and-suspenders against duplicate (tool_name, call_id) pairs across
    // a retry storm or a misbehaving provider — disambiguates atomically
    // within the process.
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    tokio::fs::create_dir_all(spill_root).await?;
    let safe_tool = sanitize_filename_component(tool_name.0.as_str());
    let safe_call = sanitize_filename_component(&call_id.0);
    let n = COUNTER.fetch_add(1, Ordering::Relaxed);
    let path = spill_root.join(format!("{safe_tool}-{safe_call}-{n}.{ext}"));
    let to_write_end = body.floor_char_boundary(body.len().min(max_spill_bytes));
    let to_write = &body[..to_write_end];
    if let Err(error) = tokio::fs::write(&path, to_write).await {
        // Best-effort cleanup so a partial ENOSPC write doesn't accumulate
        // and choke the workdir's fixed disk cap on subsequent calls.
        let _ = tokio::fs::remove_file(&path).await;
        return Err(error);
    }
    Ok((path, to_write.len()))
}

fn sanitize_filename_component(s: &str) -> String {
    let cleaned: String = s
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '-' || c == '_' {
                c
            } else {
                '_'
            }
        })
        .take(64)
        .collect();
    if cleaned.is_empty() {
        "_".into()
    } else {
        cleaned
    }
}

/// Builds the truncation envelope that always replaces an oversized tool
/// result. Must measure the *encoded* size of the candidate — cutting by raw
/// bytes underweights escape overhead for quote/backslash-heavy bodies and
/// would recreate the 413 this is meant to avoid. Shrinks the preview
/// iteratively until the envelope fits under `max_bytes`.
fn build_truncation_envelope(
    body: &str,
    ext: &str,
    max_bytes: usize,
    original_bytes: usize,
    spill: Option<&(PathBuf, usize)>,
) -> ToolOutput {
    const ITERATIONS: usize = 8;
    let format = if ext == "txt" { "text" } else { "json" };
    let mut cut = body.floor_char_boundary(body.len().min(max_bytes));
    for _ in 0..ITERATIONS {
        let candidate = compose_envelope(body, cut, format, original_bytes, spill);
        let encoded = model_bytes(&candidate).unwrap_or(usize::MAX);
        if encoded <= max_bytes {
            return candidate;
        }
        if cut == 0 {
            break;
        }
        let shrunk = (cut as f64 * (max_bytes as f64 / encoded as f64) * 0.9) as usize;
        cut = body.floor_char_boundary(shrunk.min(cut.saturating_sub(1)));
    }
    compose_envelope(body, 0, format, original_bytes, spill)
}

fn compose_envelope(
    body: &str,
    cut: usize,
    format: &str,
    original_bytes: usize,
    spill: Option<&(PathBuf, usize)>,
) -> ToolOutput {
    let mut env = serde_json::Map::new();
    env.insert("truncated".into(), Value::Bool(true));
    env.insert("original_bytes".into(), Value::from(original_bytes));
    env.insert("format".into(), Value::String(format.into()));
    env.insert("preview_bytes".into(), Value::from(cut));
    env.insert("preview".into(), Value::String(body[..cut].into()));
    if let Some((path, saved_bytes)) = spill {
        env.insert("saved_to".into(), Value::String(path.display().to_string()));
        env.insert("saved_bytes".into(), Value::from(*saved_bytes));
    }
    ToolOutput::Structured(Value::Object(env))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use agentkit_core::{Part, TextPart};
    use serde_json::json;

    fn tempdir() -> tempfile::TempDir {
        tempfile::tempdir().expect("tempdir")
    }

    fn tool_name(s: &str) -> ToolName {
        ToolName::new(s)
    }
    fn call_id(s: &str) -> ToolCallId {
        ToolCallId::new(s)
    }

    fn expect_envelope(out: &ToolOutput) -> &serde_json::Map<String, Value> {
        match out {
            ToolOutput::Structured(Value::Object(map)) => map,
            other => panic!("expected envelope, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn under_limit_output_passes_through_unchanged() {
        let dir = tempdir();
        let out = cap_output(
            ToolOutput::text("hello"),
            100,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        match out {
            ToolOutput::Text(s) => assert_eq!(s, "hello"),
            other => panic!("expected Text passthrough, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn text_over_limit_envelope_carries_preview_and_spill_pointer() {
        let dir = tempdir();
        let cap = 1024;
        let body = "a".repeat(5000);
        let out = cap_output(
            ToolOutput::text(body.clone()),
            cap,
            dir.path(),
            &tool_name("my_tool"),
            &call_id("call_abc"),
        )
        .await;
        assert!(model_bytes(&out).unwrap() <= cap);
        let map = expect_envelope(&out);

        assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
        assert_eq!(map.get("format").and_then(Value::as_str), Some("text"));
        assert_eq!(
            map.get("original_bytes").and_then(Value::as_u64),
            Some(5002)
        );

        let preview = map.get("preview").and_then(Value::as_str).expect("preview");
        assert!(preview.chars().all(|c| c == 'a'));
        assert!(body.starts_with(preview));
        assert_eq!(
            map.get("preview_bytes").and_then(Value::as_u64),
            Some(preview.len() as u64)
        );

        let saved_to = map
            .get("saved_to")
            .and_then(Value::as_str)
            .expect("saved_to");
        let saved_path = Path::new(saved_to);
        assert!(saved_path.starts_with(dir.path()));
        assert_eq!(std::fs::read_to_string(saved_path).unwrap(), body);
        assert_eq!(
            map.get("saved_bytes").and_then(Value::as_u64),
            Some(body.len() as u64)
        );
    }

    #[tokio::test]
    async fn envelope_fits_under_cap_for_quote_heavy_text() {
        // 5000 `"` chars encode to ~10 000 bytes after JSON escaping. The
        // envelope must iterate its preview cut so the encoded form still
        // fits — production caps are large enough that the envelope's fixed
        // overhead is negligible, so 1024 mirrors that property.
        let dir = tempdir();
        let cap = 1024;
        let out = cap_output(
            ToolOutput::text("\"".repeat(5000)),
            cap,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        let encoded = model_bytes(&out).expect("model_bytes");
        assert!(
            encoded <= cap,
            "envelope encoded size {encoded} <= cap {cap}"
        );
        let map = expect_envelope(&out);
        let preview = map.get("preview").and_then(Value::as_str).expect("preview");
        assert!(preview.chars().all(|c| c == '"'));
    }

    #[tokio::test]
    async fn spill_failure_still_emits_preview_envelope() {
        // Spill_root under a regular file → create_dir_all fails with
        // NotADirectory → envelope must still carry preview/original_bytes
        // without saved_to.
        let dir = tempdir();
        let blocking = dir.path().join("not-a-dir");
        std::fs::write(&blocking, b"x").expect("write blocker");
        let unreachable_root = blocking.join("spill");

        let cap = 1024;
        let out = cap_output(
            ToolOutput::text("a".repeat(5000)),
            cap,
            &unreachable_root,
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        assert!(model_bytes(&out).unwrap() <= cap);
        let map = expect_envelope(&out);
        assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
        assert!(map.get("preview").and_then(Value::as_str).is_some());
        assert!(
            map.get("saved_to").is_none(),
            "no saved_to when spill failed",
        );
    }

    #[tokio::test]
    async fn structured_over_limit_spills_pretty_json_on_disk() {
        let dir = tempdir();
        let value = json!({"items": vec!["x"; 500]});
        let content = value.to_string();
        let original = serde_json::to_string(&content).unwrap().len();
        let cap = 1024;
        let out = cap_output(
            ToolOutput::Structured(value.clone()),
            cap,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        assert!(model_bytes(&out).unwrap() <= cap);
        let map = expect_envelope(&out);
        assert_eq!(map.get("format").and_then(Value::as_str), Some("json"));
        assert_eq!(
            map.get("original_bytes").and_then(Value::as_u64),
            Some(original as u64)
        );
        let saved_to = map
            .get("saved_to")
            .and_then(Value::as_str)
            .expect("saved_to");
        assert!(saved_to.ends_with(".json"));
        let on_disk = std::fs::read_to_string(saved_to).expect("read spill");
        let parsed: Value = serde_json::from_str(&on_disk).expect("disk file is valid JSON");
        assert_eq!(parsed, value);
        assert!(
            on_disk.contains('\n'),
            "spilled JSON pretty-printed for grep/read"
        );
    }

    #[tokio::test]
    async fn parts_over_limit_yields_envelope() {
        let dir = tempdir();
        let cap = 1024;
        let parts = vec![Part::Text(TextPart::new("x".repeat(5000)))];
        let out = cap_output(
            ToolOutput::Parts(parts),
            cap,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        assert!(model_bytes(&out).unwrap() <= cap);
        let map = expect_envelope(&out);
        assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
        assert!(map.get("preview").is_some());
        assert!(map.get("saved_to").is_some());
    }

    #[tokio::test]
    async fn write_spill_truncates_body_to_max_spill_bytes() {
        let dir = tempdir();
        let body = "a".repeat(1000);
        let (path, saved) = write_spill(
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
            &body,
            "txt",
            200,
        )
        .await
        .expect("write");
        assert_eq!(saved, 200);
        assert_eq!(std::fs::read(&path).unwrap().len(), 200);
    }

    #[test]
    fn sanitize_replaces_path_separators_and_caps_length() {
        let safe = sanitize_filename_component("tool/../../etc/passwd");
        assert!(!safe.contains('/'));
        assert!(!safe.contains('.'));
        assert_eq!(sanitize_filename_component(&"x".repeat(1000)).len(), 64);
        assert_eq!(sanitize_filename_component(""), "_");
    }
}
