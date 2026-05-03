use std::path::Path;
use std::time::{Duration, Instant};

use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_tools_core::{ToolError, ToolResult};
use agentkit_tools_derive::tool;
use schemars::JsonSchema;
use serde::Deserialize;
use serde_json::json;
use tempfile::TempDir;
use tokio::process::Command;
use tokio::time::timeout;

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(600);
const STDOUT_LIMIT: usize = 50_000;
const STDERR_LIMIT: usize = 10_000;
const SCRIPT_NAME: &str = "script.ts";
const SANDBOX_ROOT: &str = "/tmp/bun-sandbox";

#[derive(Debug, Deserialize, JsonSchema)]
pub struct BunRunInput {
    /// JavaScript or TypeScript source to execute via Bun.
    pub code: String,
    /// Optional per-call wall-clock timeout in milliseconds. Defaults to 600_000.
    #[serde(default)]
    pub timeout_ms: Option<u64>,
}

#[tool(
    destructive,
    description = "Execute JavaScript or TypeScript code using the Bun runtime. \
Working directory is a fresh per-call tempdir; `playwright-core` is preinstalled. \
A headless Chromium-compatible browser (lightpanda) is available — prefer \
`import { withContext } from './browser'` and run page work inside the callback \
so each invocation gets a fresh, auto-disposed BrowserContext. The helper \
module and its `node_modules` are symlinked into the tempdir for each call. \
Use `getBrowser()` from the same module only when you need the raw Browser handle. \
For LLM-friendly page reading, `import { markdown } from './browser'` and call \
`markdown(page)` to get `{ title, byline, markdown }`; it runs Readability over \
the page HTML and serializes to Markdown. Pass `{ readable: false }` for non-article \
pages (list/index pages) where Readability extraction is not helpful."
)]
pub async fn bun_run(input: BunRunInput) -> Result<ToolResult, ToolError> {
    let workdir = TempDir::with_prefix("bun-sandbox-")
        .map_err(|e| ToolError::ExecutionFailed(format!("tempdir failed: {e}")))?;

    link_sandbox_assets(workdir.path())?;
    let file_path = workdir.path().join(SCRIPT_NAME);

    tokio::fs::write(&file_path, &input.code)
        .await
        .map_err(|e| ToolError::ExecutionFailed(format!("write failed: {e}")))?;

    let dur = input
        .timeout_ms
        .map(Duration::from_millis)
        .unwrap_or(DEFAULT_TIMEOUT);

    let start = Instant::now();

    let mut cmd = Command::new("/usr/local/bin/bun");
    cmd.arg("run").arg(SCRIPT_NAME).current_dir(workdir.path());
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

fn link_sandbox_assets(workdir: &Path) -> Result<(), ToolError> {
    for name in ["browser.ts", "package.json", "node_modules"] {
        let src = Path::new(SANDBOX_ROOT).join(name);
        let dst = workdir.join(name);
        symlink(&src, &dst).map_err(|e| {
            ToolError::ExecutionFailed(format!(
                "link sandbox asset {} -> {} failed: {e}",
                src.display(),
                dst.display()
            ))
        })?;
    }
    Ok(())
}

#[cfg(unix)]
fn symlink(src: &Path, dst: &Path) -> std::io::Result<()> {
    std::os::unix::fs::symlink(src, dst)
}

#[cfg(windows)]
fn symlink(src: &Path, dst: &Path) -> std::io::Result<()> {
    if src.is_dir() {
        std::fs::symlink_dir(src, dst)
    } else {
        std::fs::symlink_file(src, dst)
    }
}
