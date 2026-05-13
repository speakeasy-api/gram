use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_tools_core::{ToolError, ToolResult};
use agentkit_tools_derive::tool;
use schemars::JsonSchema;
use serde::Deserialize;
use serde_json::json;
use tempfile::NamedTempFile;
use tokio::process::Command;
use tokio::time::timeout;

use crate::workdir::{ASSISTANT_WORKDIR, canonicalize_inside_workdir};

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(600);
const STDOUT_LIMIT: usize = 50_000;
const STDERR_LIMIT: usize = 10_000;

#[derive(Debug, Deserialize, JsonSchema)]
pub struct BunRunInput {
    /// Inline JavaScript or TypeScript source to execute. Mutually exclusive
    /// with `file`; exactly one of the two must be supplied.
    #[serde(default)]
    pub code: Option<String>,
    /// Absolute path to a JavaScript or TypeScript file inside the assistant
    /// workdir. The file is run by Bun as-is. Mutually exclusive with `code`.
    #[serde(default)]
    pub file: Option<String>,
    /// Optional per-call wall-clock timeout in milliseconds. Defaults to 600_000.
    #[serde(default)]
    pub timeout_ms: Option<u64>,
}

#[tool(
    destructive,
    description = "Execute JavaScript or TypeScript with the Bun runtime. \
Working directory is the persistent assistant workdir; files written there \
via the filesystem tools persist across calls for the lifetime of the \
assistant VM. Pass `file` (an absolute path inside the workdir) to execute \
an existing file, or `code` to run inline source. Exactly one of the two is \
required. \
`playwright-core` is preinstalled and a headless Chromium-compatible browser \
(lightpanda) is available — prefer `import { withContext } from './browser'` \
and run page work inside the callback so each invocation gets a fresh, \
auto-disposed BrowserContext. Use `getBrowser()` from the same module only \
when you need the raw Browser handle. For LLM-friendly page reading, \
`import { markdown } from './browser'` and call `markdown(page)` to get \
`{ title, byline, markdown }`; it runs Readability over the page HTML and \
serializes to Markdown. Pass `{ readable: false }` for non-article pages \
(list/index pages) where Readability extraction is not helpful."
)]
pub async fn bun_run(input: BunRunInput) -> Result<ToolResult, ToolError> {
    let script = resolve_script(&input).await?;

    let dur = input
        .timeout_ms
        .map(Duration::from_millis)
        .unwrap_or(DEFAULT_TIMEOUT);

    let start = Instant::now();

    let mut cmd = Command::new("/usr/local/bin/bun");
    cmd.arg("run")
        .arg(script.path())
        .current_dir(ASSISTANT_WORKDIR);
    cmd.env("NO_COLOR", "1");
    cmd.kill_on_drop(true);

    let output = timeout(dur, cmd.output())
        .await
        .map_err(|_| {
            ToolError::ExecutionFailed(format!("bun timed out after {}ms", dur.as_millis()))
        })?
        .map_err(|e| ToolError::ExecutionFailed(format!("spawn failed: {e}")))?;

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    let stdout_truncated = stdout.len() > STDOUT_LIMIT;
    let stderr_truncated = stderr.len() > STDERR_LIMIT;
    let exit_code = output.status.code();

    Ok(ToolResult {
        result: ToolResultPart {
            call_id: Default::default(),
            output: ToolOutput::Structured(json!({
                "stdout": &stdout[..stdout.floor_char_boundary(STDOUT_LIMIT)],
                "stderr": &stderr[..stderr.floor_char_boundary(STDERR_LIMIT)],
                "truncated": stdout_truncated || stderr_truncated,
                "exit_code": exit_code,
                "success": output.status.success(),
            })),
            is_error: !output.status.success(),
            metadata: MetadataMap::new(),
        },
        duration: Some(start.elapsed()),
        metadata: MetadataMap::new(),
    })
}

#[derive(Debug)]
enum Script {
    /// O_EXCL temp file inside the persistent workdir; deleted on Drop so
    /// inline calls don't accumulate `.bun-inline-*.ts` cruft.
    Inline(NamedTempFile),
    File(PathBuf),
}

impl Script {
    fn path(&self) -> &Path {
        match self {
            Script::Inline(f) => f.path(),
            Script::File(p) => p,
        }
    }
}

async fn resolve_script(input: &BunRunInput) -> Result<Script, ToolError> {
    match (&input.code, &input.file) {
        (Some(_), Some(_)) => Err(ToolError::InvalidInput(
            "bun_run accepts exactly one of `code` or `file`, not both".into(),
        )),
        (None, None) => Err(ToolError::InvalidInput(
            "bun_run requires either `code` or `file`".into(),
        )),
        (Some(code), None) => {
            let tmp = tempfile::Builder::new()
                .prefix(".bun-inline-")
                .suffix(".ts")
                .tempfile_in(ASSISTANT_WORKDIR)
                .map_err(|e| {
                    ToolError::ExecutionFailed(format!("create inline script: {e}"))
                })?;
            tokio::fs::write(tmp.path(), code)
                .await
                .map_err(|e| ToolError::ExecutionFailed(format!("write inline script: {e}")))?;
            Ok(Script::Inline(tmp))
        }
        (None, Some(file)) => {
            canonicalize_inside_workdir(Path::new(file)).map(Script::File)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn rejects_input_with_both_code_and_file() {
        let input = BunRunInput {
            code: Some("console.log(1)".into()),
            file: Some("/tmp/x.ts".into()),
            timeout_ms: None,
        };
        let err = resolve_script(&input).await.expect_err("both must be rejected");
        match err {
            ToolError::InvalidInput(msg) => assert!(msg.contains("not both")),
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn rejects_input_with_neither_code_nor_file() {
        let input = BunRunInput {
            code: None,
            file: None,
            timeout_ms: None,
        };
        let err = resolve_script(&input).await.expect_err("neither must be rejected");
        match err {
            ToolError::InvalidInput(msg) => assert!(msg.contains("requires")),
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }
}
