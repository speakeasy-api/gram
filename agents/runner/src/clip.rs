//! Per-tool-result byte cap for the assistant runtime.
//!
//! OpenRouter caps a single tool message at 204800 bytes and 413s the next
//! request when a tool result exceeds it. Token-aware compaction can't catch
//! this: the trigger reads `usage.input_tokens` after the previous turn, and
//! 200 KB ≈ 50 K tokens — well below the 80 % threshold for a 128 K+ window.
//! Even when it does fire, [`SummarizeOlderStrategy`](agentkit_compaction)
//! preserves the most recent items, so the offending tool result survives.
//!
//! When a tool result exceeds [`MAX_TOOL_BYTES`], the full body is written
//! to a file inside the assistant workdir — where the agent's filesystem
//! tools can read or grep it — and the in-band result is replaced with a
//! small envelope pointing at that file. No information is lost; the model
//! follows up by reading the saved file. If the spill itself fails (disk
//! full, permission denied), the body is clipped in place with a marker so
//! the provider still gets a valid, sub-cap result.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use agentkit_core::{ToolCallId, ToolOutput};
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
    match write_spill(spill_root, tool_name, call_id, &body, ext, MAX_SPILL_BYTES).await {
        Ok((path, saved_bytes)) => ToolOutput::Structured(json!({
            "truncated": true,
            "saved_to": path.display().to_string(),
            "original_bytes": model_bytes,
            "saved_bytes": saved_bytes,
        })),
        Err(error) => {
            tracing::warn!(
                error = %error,
                spill_root = %spill_root.display(),
                "spill of oversized tool result failed; clipping inline as fallback",
            );
            clip_inline(body, ext, max_bytes, model_bytes)
        }
    }
}

/// JSON-encoded byte size of the *content* the provider adapter sends as
/// the tool message body. For `Text`, this is `"…"` after escaping — counting
/// raw `s.len()` would undercount escape overhead, so a quote-heavy 150 KB
/// blob could be ~300 KB on the wire and still 413 the provider. Returns
/// `None` only if serialization itself fails — in that case the cap can't
/// be enforced and the output is returned untouched.
fn model_bytes(output: &ToolOutput) -> Option<usize> {
    match output {
        ToolOutput::Text(s) => serde_json::to_string(s).ok().map(|j| j.len()),
        ToolOutput::Structured(v) => serde_json::to_string(v).ok().map(|j| j.len()),
        ToolOutput::Parts(p) => serde_json::to_string(p).ok().map(|j| j.len()),
        ToolOutput::Files(f) => serde_json::to_string(f).ok().map(|j| j.len()),
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

/// Fallback when the disk spill fails. Drops the tail of the body so the
/// provider still receives a sub-cap result. Must measure the *encoded*
/// size of the candidate output — cutting by raw bytes underweights escape
/// overhead for quote/backslash-heavy bodies, and would recreate the 413
/// the spill was meant to avoid. Shrinks iteratively until the encoded
/// candidate fits under `max_bytes`.
fn clip_inline(body: String, ext: &str, max_bytes: usize, original_bytes: usize) -> ToolOutput {
    const ITERATIONS: usize = 8;
    let mut cut = body.floor_char_boundary(body.len().min(max_bytes));
    for _ in 0..ITERATIONS {
        let candidate = build_clipped(&body, cut, ext, original_bytes);
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
    build_clipped(&body, 0, ext, original_bytes)
}

fn build_clipped(body: &str, cut: usize, ext: &str, original_bytes: usize) -> ToolOutput {
    if ext == "txt" {
        let dropped = body.len() - cut;
        ToolOutput::Text(format!("{}…[truncated {dropped} bytes]", &body[..cut]))
    } else {
        ToolOutput::Structured(json!({
            "truncated": true,
            "original_bytes": original_bytes,
            "kept_bytes": cut,
            "content": &body[..cut],
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use agentkit_core::{Part, TextPart};
    use serde_json::{Value, json};

    fn tempdir() -> tempfile::TempDir {
        tempfile::tempdir().expect("tempdir")
    }

    fn tool_name(s: &str) -> ToolName {
        ToolName::new(s)
    }
    fn call_id(s: &str) -> ToolCallId {
        ToolCallId::new(s)
    }

    #[tokio::test]
    async fn text_under_limit_passes_through() {
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
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn text_over_limit_spills_to_file() {
        let dir = tempdir();
        let body = "a".repeat(500);
        let out = cap_output(
            ToolOutput::text(body.clone()),
            100,
            dir.path(),
            &tool_name("my_tool"),
            &call_id("call_abc"),
        )
        .await;
        let map = match out {
            ToolOutput::Structured(Value::Object(map)) => map,
            other => panic!("expected envelope, got {other:?}"),
        };
        assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
        // 500 plain `a` bytes encode to `"aaa..."` = 502 bytes after the
        // surrounding quote literals (no escapes needed).
        assert_eq!(
            map.get("original_bytes").and_then(Value::as_u64),
            Some(502)
        );
        let saved_to = map
            .get("saved_to")
            .and_then(Value::as_str)
            .expect("saved_to");
        let saved_path = Path::new(saved_to);
        assert!(saved_path.starts_with(dir.path()));
        let on_disk = std::fs::read_to_string(saved_path).expect("read spill");
        assert_eq!(on_disk, body);
    }

    #[tokio::test]
    async fn structured_over_limit_spills_pretty_json() {
        let dir = tempdir();
        let value = json!({"items": vec!["x"; 500]});
        let original = serde_json::to_string(&value).unwrap().len();
        let out = cap_output(
            ToolOutput::Structured(value.clone()),
            200,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        let map = match out {
            ToolOutput::Structured(Value::Object(map)) => map,
            other => panic!("expected envelope, got {other:?}"),
        };
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
            "spilled JSON should be pretty-printed for grep/read"
        );
    }

    #[tokio::test]
    async fn quote_heavy_text_spills_even_when_raw_len_fits() {
        // 100 raw `"` bytes encode to 200 bytes (`\"` for each) plus the
        // surrounding `"…"` literal. With a 150-byte cap the raw length is
        // under the limit but the JSON-encoded form is not — the spill must
        // still fire.
        let dir = tempdir();
        let body = "\"".repeat(100);
        assert!(body.len() < 150);
        let out = cap_output(
            ToolOutput::text(body.clone()),
            150,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        match out {
            ToolOutput::Structured(Value::Object(map)) => {
                assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
                let saved_to = map.get("saved_to").and_then(Value::as_str).unwrap();
                assert_eq!(std::fs::read_to_string(saved_to).unwrap(), body);
            }
            other => panic!("expected spill envelope, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn write_spill_truncates_body_to_max_spill_bytes() {
        let dir = tempdir();
        let body = "a".repeat(1000);
        let (path, saved) =
            write_spill(dir.path(), &tool_name("t"), &call_id("c"), &body, "txt", 200)
                .await
                .expect("write");
        assert_eq!(saved, 200);
        let on_disk = std::fs::read(&path).expect("read");
        assert_eq!(on_disk.len(), 200);
    }

    #[tokio::test]
    async fn parts_over_limit_spills() {
        let dir = tempdir();
        let body = "x".repeat(500);
        let parts = vec![Part::Text(TextPart::new(body))];
        let out = cap_output(
            ToolOutput::Parts(parts),
            100,
            dir.path(),
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        match out {
            ToolOutput::Structured(Value::Object(map)) => {
                assert_eq!(map.get("truncated"), Some(&Value::Bool(true)));
                assert!(map.get("saved_to").is_some());
            }
            other => panic!("expected envelope, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn spill_failure_falls_back_to_inline_clip() {
        // Make spill_root a path under a regular file — create_dir_all will
        // bail with NotADirectory, exercising the fallback branch.
        let dir = tempdir();
        let blocking = dir.path().join("not-a-dir");
        std::fs::write(&blocking, b"x").expect("write blocker");
        let unreachable_root = blocking.join("spill");

        let body = "a".repeat(500);
        let out = cap_output(
            ToolOutput::text(body),
            100,
            &unreachable_root,
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        let s = match out {
            ToolOutput::Text(s) => s,
            other => panic!("expected Text fallback, got {other:?}"),
        };
        assert!(s.contains("truncated"), "marker present: {s}");
        assert!(s.len() < 500, "tail dropped");
    }

    #[test]
    fn sanitize_replaces_path_separators_and_caps_length() {
        let dirty = "tool/../../etc/passwd";
        let safe = sanitize_filename_component(dirty);
        assert!(!safe.contains('/'));
        assert!(!safe.contains('.'));

        let long = "x".repeat(1000);
        let safe_long = sanitize_filename_component(&long);
        assert_eq!(safe_long.len(), 64);

        assert_eq!(sanitize_filename_component(""), "_");
    }

    #[tokio::test]
    async fn fallback_clip_respects_encoded_size_for_quote_heavy_text() {
        // 500 `"` chars encode to ~1000 bytes after JSON escaping. With a
        // 150-byte cap and the spill path unreachable, the inline fallback
        // must shrink the raw cut so the encoded ToolOutput still fits —
        // otherwise it recreates the very 413 the spill was meant to avoid.
        let dir = tempdir();
        let blocking = dir.path().join("not-a-dir");
        std::fs::write(&blocking, b"x").expect("write blocker");
        let unreachable_root = blocking.join("spill");

        let out = cap_output(
            ToolOutput::text("\"".repeat(500)),
            150,
            &unreachable_root,
            &tool_name("t"),
            &call_id("c"),
        )
        .await;
        let encoded = model_bytes(&out).expect("model_bytes");
        assert!(
            encoded <= 150,
            "fallback encoded size {encoded} must fit under cap 150"
        );
        match out {
            ToolOutput::Text(s) => assert!(s.contains("truncated"), "marker present: {s}"),
            other => panic!("expected Text fallback, got {other:?}"),
        }
    }

    #[test]
    fn clip_inline_text_appends_marker_and_stays_under_cap() {
        let out = clip_inline("a".repeat(200), "txt", 100, 200);
        let encoded = model_bytes(&out).expect("encode");
        assert!(encoded <= 100, "encoded {encoded} under cap 100");
        match out {
            ToolOutput::Text(s) => {
                assert!(s.contains("truncated"), "marker present: {s}");
                assert!(s.starts_with('a'), "kept prefix is original content");
            }
            other => panic!("expected Text, got {other:?}"),
        }
    }

    #[test]
    fn clip_inline_text_respects_utf8_boundary() {
        // 50 × "é" = 100 bytes. The clipper hops back from any cut point that
        // would split a 2-byte codepoint.
        let out = clip_inline("é".repeat(50), "txt", 75, 100);
        match out {
            ToolOutput::Text(s) => {
                let prefix = s.split('…').next().unwrap();
                assert!(prefix.chars().all(|c| c == 'é'));
                assert!(prefix.len() <= 75);
            }
            other => panic!("expected Text, got {other:?}"),
        }
    }
}
