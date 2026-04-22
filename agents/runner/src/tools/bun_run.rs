use std::time::{Duration, Instant};

use agentkit_core::{MetadataMap, ToolOutput, ToolResultPart};
use agentkit_tools_core::{
    Tool, ToolAnnotations, ToolContext, ToolError, ToolName, ToolRegistry, ToolRequest, ToolResult,
    ToolSpec,
};
use async_trait::async_trait;
use serde::Deserialize;
use serde_json::json;
use tempfile::TempDir;
use tokio::process::Command;
use tokio::time::timeout;

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(600);
const STDOUT_LIMIT: usize = 50_000;
const STDERR_LIMIT: usize = 10_000;

pub fn registry() -> ToolRegistry {
    ToolRegistry::new().with(BunRunTool::default())
}

#[derive(Clone, Debug)]
pub struct BunRunTool {
    spec: ToolSpec,
}

impl Default for BunRunTool {
    fn default() -> Self {
        Self {
            spec: ToolSpec {
                name: ToolName::new("bun_run"),
                description: "Execute JavaScript or TypeScript code using the Bun runtime. \
Working directory is a fresh per-call tempdir; `playwright-core` is preinstalled. \
A headless Chromium-compatible browser (lightpanda) is available — prefer \
`import { withContext } from './browser'` and run page work inside the callback \
so each invocation gets a fresh, auto-disposed BrowserContext. \
Use `getBrowser()` from the same module only when you need the raw Browser handle. \
For LLM-friendly page reading, `import { markdown } from './browser'` and call \
`markdown(page)` to get `{ title, byline, markdown }`; it runs Readability over \
the page HTML and serializes to Markdown. Pass `{ readable: false }` for non-article \
pages (list/index pages) where Readability extraction is not helpful."
                    .into(),
                input_schema: json!({
                    "type": "object",
                    "properties": {
                        "code": { "type": "string" },
                        "filename": { "type": "string" },
                        "timeout_ms": { "type": "integer", "minimum": 1 }
                    },
                    "required": ["code"],
                    "additionalProperties": false
                }),
                annotations: ToolAnnotations {
                    destructive_hint: true,
                    needs_approval_hint: false,
                    ..ToolAnnotations::default()
                },
                metadata: MetadataMap::new(),
            },
        }
    }
}

#[derive(Debug, Deserialize)]
struct BunRunInput {
    code: String,
    filename: Option<String>,
    timeout_ms: Option<u64>,
}

#[async_trait]
impl Tool for BunRunTool {
    fn spec(&self) -> &ToolSpec {
        &self.spec
    }

    async fn invoke(
        &self,
        request: ToolRequest,
        _ctx: &mut ToolContext<'_>,
    ) -> Result<ToolResult, ToolError> {
        let input: BunRunInput = serde_json::from_value(request.input.clone())
            .map_err(|e| ToolError::InvalidInput(format!("invalid bun_run input: {e}")))?;

        let workdir = TempDir::with_prefix("bun-sandbox-")
            .map_err(|e| ToolError::ExecutionFailed(format!("tempdir failed: {e}")))?;

        let filename = input
            .filename
            .unwrap_or_else(|| format!("script-{}.ts", std::process::id()));
        let file_path = workdir.path().join(&filename);

        tokio::fs::write(&file_path, &input.code)
            .await
            .map_err(|e| ToolError::ExecutionFailed(format!("write failed: {e}")))?;

        let dur = input
            .timeout_ms
            .map(Duration::from_millis)
            .unwrap_or(DEFAULT_TIMEOUT);

        let start = Instant::now();

        let mut cmd = Command::new("/usr/local/bin/bun");
        cmd.arg("run").arg(&file_path).current_dir(workdir.path());
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
                call_id: request.call_id,
                output: ToolOutput::Structured(json!({
                    "stdout": &stdout[..stdout.len().min(STDOUT_LIMIT)],
                    "stderr": &stderr[..stderr.len().min(STDERR_LIMIT)],
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
}
