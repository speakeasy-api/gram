use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_core::{Item, ItemKind, Part, TextPart, ToolCallPart, ToolOutput, ToolResultPart};
use agentkit_loop::{
    Agent, LoopDriver, LoopInterrupt, LoopStep, ModelSession, PromptCacheRequest,
    PromptCacheRetention, SessionConfig,
};
use agentkit_mcp::{
    McpServerConfig, McpServerId, McpServerManager, McpTransportBinding,
    StreamableHttpTransportConfig,
};
use agentkit_provider_openrouter::{OpenRouterConfig, OpenRouterProvider};
use agentkit_reporting::TracingReporter;
use agentkit_tool_fs::{FileSystemToolPolicy, FileSystemToolResources};
use agentkit_tools_core::{
    CompositePermissionChecker, PathPolicy, PermissionDecision, ToolRegistry,
};
use serde_json::Value;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};
use tokio::sync::{Mutex as AsyncMutex, Notify, oneshot};

use agentkit_compaction::AgentBuilderCompactorExt;

use crate::clip::ClippedToolSource;
use crate::compaction::build_compactor;
use crate::errors::RunnerError;
use crate::http_layer::{McpRotatingClient, TokenRegistry, build_http};
use crate::idempotency::IdempotencyCache;
use crate::tools;
use crate::wire::{McpServer, RunnerConfig, RunnerMessage, RunnerRequest};
use crate::workdir::ASSISTANT_WORKDIR;

const MCP_CMD_CAPACITY: usize = 32;

/// Maximum time the agent loop waits for `connect_all` before serving its first
/// turn. Bounded so a single slow server can't gate the whole runtime; servers
/// still connecting after this deadline keep registering their tools as they
/// come online via the catalog reader handed to the agent.
const MCP_STARTUP_DEADLINE: Duration = Duration::from_secs(3);

pub type AppState = Arc<AsyncMutex<RuntimeHost>>;

/// Commands routed to the MCP manager actor task. Instead of sharing the
/// `McpServerManager` behind a mutex, we keep it private to a single task and
/// drive it through this channel; the parent only ever sees the read-side
/// [`agentkit_tools_core::CatalogReader`] returned by `manager.source()`.
pub enum McpCmd {
    /// Force a server to disconnect and reconnect from scratch. Surfaced to
    /// the assistant via the `mcp_force_reconnect` tool so the model can
    /// recover from transport-level errors without operator intervention.
    ForceReconnect {
        server_id: McpServerId,
        reply: oneshot::Sender<Result<(), String>>,
    },
}

pub struct RuntimeHost {
    pub runtime: Option<ConfiguredRuntime>,
    pub seen: IdempotencyCache,
    /// Signals the HTTP server to shut down. Loop task fires this when it
    /// exits (warm TTL / fatal error) so the process tears down immediately
    /// rather than lingering in a configured-but-dead state.
    pub shutdown: Arc<Notify>,
}

pub struct ConfiguredRuntime {
    tokens: TokenRegistry,
    inbox_tx: UnboundedSender<String>,
    /// `None` while a turn is in flight; `Some(t)` when idle since `t`. The two
    /// signals are inherently exclusive — a single optional instant prevents
    /// representing "in-flight AND idle since X" by construction.
    idle_since: Arc<Mutex<Option<Instant>>>,
    // Non-rotating fields of the RunnerConfig this runtime was built from.
    // `auth_token` is excluded — it rolls on every /turn — so a caller retrying
    // /configure with a refreshed token is still treated as an identical config.
    fingerprint: u64,
    _mcp_actor: tokio::task::JoinHandle<()>,
    _loop_handle: tokio::task::JoinHandle<()>,
}

impl ConfiguredRuntime {
    pub fn idle_for(&self) -> Option<Duration> {
        let guard = self.idle_since.lock().ok()?;
        Some(match *guard {
            None => Duration::ZERO,
            Some(t) => Instant::now().saturating_duration_since(t),
        })
    }

    pub fn rotate_token(&self, token: &str) -> Result<(), RunnerError> {
        self.tokens.rotate(token)
    }

    pub fn matches(&self, config: &RunnerConfig) -> bool {
        self.fingerprint == fingerprint(config)
    }

    pub fn enqueue(&self, request: RunnerRequest) -> Result<(), RunnerError> {
        self.inbox_tx
            .send(request.input)
            .map_err(|_| RunnerError::SubmitInput("loop inbox closed".into()))?;
        // Mark busy synchronously so /state can't report a stale idle window
        // between enqueue and the loop's mark_busy on AwaitingInput.
        mark_busy(&self.idle_since);
        Ok(())
    }
}

fn fingerprint(config: &RunnerConfig) -> u64 {
    use std::hash::{DefaultHasher, Hash, Hasher};
    let mut hasher = DefaultHasher::new();
    config.model.hash(&mut hasher);
    config.instructions.hash(&mut hasher);
    config.completions_url.hash(&mut hasher);
    config.chat_id.hash(&mut hasher);
    config.mcp_servers.hash(&mut hasher);
    config.history.hash(&mut hasher);
    hasher.finish()
}

pub async fn build_runtime(
    config: &RunnerConfig,
    shutdown: Arc<Notify>,
) -> Result<ConfiguredRuntime, RunnerError> {
    tracing::info!(
        model = %config.model,
        mcp_servers = config.mcp_servers.len(),
        chat_id = %config.chat_id,
        "build_runtime"
    );

    let tokens = TokenRegistry::new(config.auth_token.clone());

    let mut default_headers = http::HeaderMap::new();
    default_headers.insert(
        http::HeaderName::from_static("gram-chat-id"),
        http::HeaderValue::from_str(&config.chat_id)
            .map_err(|source| RunnerError::HeaderValue { source })?,
    );
    default_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );

    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .build()?;

    let mut manager = McpServerManager::new();
    let mut mcp_server_ids = Vec::with_capacity(config.mcp_servers.len());
    for server in &config.mcp_servers {
        mcp_server_ids.push(server.id.clone());
        manager.register_server(build_mcp_server_config(server, &http_client, &tokens)?);
    }
    let mcp_source = ClippedToolSource::new(manager.source());

    let (mcp_cmd_tx, mcp_cmd_rx) = mpsc::channel(MCP_CMD_CAPACITY);
    let (mcp_ready_tx, mcp_ready_rx) = oneshot::channel::<()>();
    let mcp_actor = tokio::spawn(run_mcp_actor(manager, mcp_cmd_rx, mcp_ready_tx));

    let native_tools = ToolRegistry::new().with(tools::bun_run::bun_run).with(
        tools::mcp_force_reconnect::McpForceReconnectTool::new(mcp_cmd_tx.clone(), mcp_server_ids),
    );

    // Sandbox helpers stay readable so user code can `import` them, but writes
    // to those paths must fail so an assistant can't shadow `browser.ts` etc.
    let permissions = CompositePermissionChecker::new(PermissionDecision::Allow).with_policy(
        PathPolicy::new()
            .allow_root(ASSISTANT_WORKDIR)
            .read_only_root(format!("{ASSISTANT_WORKDIR}/node_modules"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/browser.ts"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/package.json")),
    );

    let fs_resources = FileSystemToolResources::new()
        .with_policy(FileSystemToolPolicy::new().require_read_before_write(true));

    let base_url = config
        .completions_url
        .clone()
        .ok_or_else(|| RunnerError::ConfigError {
            key: "completions_url".to_string(),
        })?;
    let openrouter_config =
        OpenRouterConfig::new(String::new(), config.model.clone()).with_base_url(base_url);
    let provider = OpenRouterProvider::from(openrouter_config);

    let completions_http = build_http(http_client.clone(), tokens.clone());
    let adapter = CompletionsAdapter::with_client(provider.clone(), completions_http);

    // Compactor calls reuse the model and bearer rotation but must omit the
    // Gram-Chat-ID header so they bypass chat-message capture; otherwise gram
    // treats the compactor's "summarise this transcript" turn as a divergence
    // and the next replay loads the compactor's transcript instead of the
    // user's.
    let mut compactor_headers = http::HeaderMap::new();
    compactor_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let compactor_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(compactor_headers)
        .build()?;
    let compactor_http = build_http(compactor_http_client, tokens.clone());
    let compactor_adapter = CompletionsAdapter::with_client(provider, compactor_http);

    let compactor = build_compactor(
        &config.chat_id,
        config.context_window.unwrap_or(0),
        compactor_adapter,
    )?;

    let mut transcript = Vec::new();
    if let Some(instructions) = &config.instructions {
        transcript.push(Item::text(ItemKind::System, instructions));
    }
    transcript.extend(normalize_history(&config.history)?);

    let mut builder = Agent::builder()
        .model(adapter)
        .add_tool_source(native_tools)
        .add_tool_source(agentkit_tool_fs::registry())
        .add_tool_source(mcp_source)
        .permissions(permissions)
        .resources(fs_resources)
        .observer(TracingReporter::new())
        .transcript(transcript);

    if let Some(compactor) = compactor {
        builder = builder.compactor(compactor);
    }

    let agent = builder
        .build()
        .map_err(|e| RunnerError::AgentBuild(e.to_string()))?;

    let session = SessionConfig::new(config.chat_id.clone())
        .with_cache(PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short));

    let driver = agent
        .start(session)
        .await
        .map_err(|e| RunnerError::AgentStart(e.to_string()))?;

    let idle_since = Arc::new(Mutex::new(Some(Instant::now())));

    let (inbox_tx, inbox_rx) = mpsc::unbounded_channel::<String>();

    let loop_idle_since = Arc::clone(&idle_since);
    let loop_handle = tokio::spawn(async move {
        let outcome = run_loop(driver, inbox_rx, loop_idle_since, mcp_ready_rx).await;
        match outcome {
            Ok(reason) => tracing::info!(reason = %reason, "loop exited"),
            Err(err) => tracing::error!(error = %err, "loop exited with error"),
        }
        shutdown.notify_waiters();
    });

    tracing::info!("build_runtime ok");

    Ok(ConfiguredRuntime {
        tokens,
        inbox_tx,
        idle_since,
        fingerprint: fingerprint(config),
        _mcp_actor: mcp_actor,
        _loop_handle: loop_handle,
    })
}

fn build_mcp_server_config(
    server: &McpServer,
    http_client: &reqwest::Client,
    tokens: &TokenRegistry,
) -> Result<McpServerConfig, RunnerError> {
    let mut server_headers = http::HeaderMap::new();
    for (k, v) in &server.headers {
        let name = http::HeaderName::from_bytes(k.as_bytes()).map_err(|source| {
            RunnerError::McpHeaderName {
                server: server.id.clone(),
                name: k.clone(),
                source,
            }
        })?;
        let value =
            http::HeaderValue::from_str(v).map_err(|source| RunnerError::McpHeaderValue {
                server: server.id.clone(),
                name: k.clone(),
                source,
            })?;
        server_headers.insert(name, value);
    }
    let mcp_http = Arc::new(McpRotatingClient::new(
        http_client.clone(),
        tokens.clone(),
        server_headers,
    ));
    let transport = StreamableHttpTransportConfig::new(&server.url).with_http_client(mcp_http);
    Ok(McpServerConfig::new(
        &server.id,
        McpTransportBinding::StreamableHttp(transport),
    ))
}

/// Owns the [`McpServerManager`] for the lifetime of a runtime. Connects every
/// registered server in the background — `/configure` does not wait — and
/// processes [`McpCmd`]s serially so the manager never needs to be shared.
///
/// `ready_tx` resolves once `connect_all` returns (success or aggregate error),
/// letting the loop start its first turn with a fully-populated tool registry
/// rather than racing the model against in-flight handshakes. Subsequent
/// reconnects do not re-arm it — late tools register through the catalog
/// reader the same way they do today.
async fn run_mcp_actor(
    mut manager: McpServerManager,
    mut cmd_rx: mpsc::Receiver<McpCmd>,
    ready_tx: oneshot::Sender<()>,
) {
    match manager.connect_all().await {
        Ok(handles) => tracing::info!(servers = handles.len(), "mcp connect_all ok"),
        Err(e) => tracing::warn!(
            error = %e,
            "mcp connect_all failed; affected tools will surface errors and the model can call mcp_force_reconnect"
        ),
    }
    let _ = ready_tx.send(());

    while let Some(cmd) = cmd_rx.recv().await {
        match cmd {
            McpCmd::ForceReconnect { server_id, reply } => {
                if let Err(e) = manager.disconnect_server(&server_id).await {
                    tracing::debug!(server_id = %server_id, error = %e, "disconnect during force reconnect");
                }
                let result = match manager.connect_server(&server_id).await {
                    Ok(handle) => {
                        tracing::info!(
                            server_id = %server_id,
                            tools = handle.snapshot().tools.len(),
                            "mcp force reconnect ok"
                        );
                        Ok(())
                    }
                    Err(e) => {
                        tracing::warn!(server_id = %server_id, error = %e, "mcp force reconnect failed");
                        Err(e.to_string())
                    }
                };
                let _ = reply.send(result);
            }
        }
    }
}

/// Drives the agent loop until the inbox closes or a fatal error occurs.
/// Lifecycle (warm-window eviction, shutdown) is owned by the backend, which
/// polls `/state` for `idle_seconds` and stops the runner externally.
///
/// Input arrives via `inbox`; `/turn` pushes there and returns immediately.
/// The driver's transcript is preloaded with instructions + history, so the
/// very first `next()` yields `AwaitingInput` and the first `/turn` supplies
/// the first user message — same code path as every later turn.
///
/// Agent loop events the runner cares about:
/// - `LoopStep::Finished`: turn ended. Mark idle and loop back into `next()`.
/// - `LoopInterrupt::AwaitingInput`: drain queued inputs and submit, or block
///   on the inbox until the next message (or close).
/// - `LoopInterrupt::AfterToolResult`: cooperative mid-turn yield. Drain any
///   queued inputs and submit before the next model call.
/// - `LoopInterrupt::ApprovalRequest`: tools in this environment should not
///   require approval. Warn and auto-approve so we don't deadlock.
async fn run_loop<S>(
    mut driver: LoopDriver<S>,
    mut inbox: UnboundedReceiver<String>,
    idle_since: Arc<Mutex<Option<Instant>>>,
    mcp_ready_rx: oneshot::Receiver<()>,
) -> Result<&'static str, RunnerError>
where
    S: ModelSession,
{
    // Consumed before the first `req.submit` and never re-armed. Late servers
    // surface their tools through the catalog reader without further blocking.
    let mut mcp_ready: Option<oneshot::Receiver<()>> = Some(mcp_ready_rx);
    loop {
        match driver.next().await? {
            LoopStep::Finished(_turn) => {
                mark_idle(&idle_since);
            }
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(req)) => {
                let drained = drain(&mut inbox);
                let items = if !drained.is_empty() {
                    drained_into_items(drained)
                } else {
                    match inbox.recv().await {
                        Some(msg) => vec![Item::text(ItemKind::User, &msg)],
                        None => return Ok("inbox closed"),
                    }
                };
                if let Some(rx) = mcp_ready.take() {
                    await_mcp_ready(rx).await;
                }
                mark_busy(&idle_since);
                req.submit(&mut driver, items)?;
            }
            LoopStep::Interrupt(LoopInterrupt::AfterToolResult(info)) => {
                let drained = drain(&mut inbox);
                if !drained.is_empty() {
                    info.submit(&mut driver, drained_into_items(drained))?;
                }
            }
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(pending)) => {
                tracing::warn!(
                    "unexpected approval request — runner auto-approves; tools should \
                     not require approval in this environment"
                );
                pending.approve(&mut driver)?;
            }
        }
    }
}

/// Wait for the MCP actor's connect_all to finish, capped at
/// `MCP_STARTUP_DEADLINE`. Logs which side won so an operator can correlate a
/// thin first-turn registry with a slow upstream.
async fn await_mcp_ready(rx: oneshot::Receiver<()>) {
    match tokio::time::timeout(MCP_STARTUP_DEADLINE, rx).await {
        Ok(Ok(())) => tracing::debug!("mcp ready before first turn"),
        Ok(Err(_)) => tracing::debug!("mcp actor dropped ready signal; proceeding"),
        Err(_) => tracing::warn!(
            deadline_ms = MCP_STARTUP_DEADLINE.as_millis() as u64,
            "mcp connect_all deadline hit; first turn proceeds with partial tool registry"
        ),
    }
}

fn drained_into_items(drained: Vec<String>) -> Vec<Item> {
    drained
        .into_iter()
        .map(|s| Item::text(ItemKind::User, &s))
        .collect()
}

fn drain(inbox: &mut UnboundedReceiver<String>) -> Vec<String> {
    let mut out = Vec::new();
    while let Ok(msg) = inbox.try_recv() {
        out.push(msg);
    }
    out
}

fn mark_busy(idle_since: &Arc<Mutex<Option<Instant>>>) {
    if let Ok(mut slot) = idle_since.lock() {
        *slot = None;
    }
}

fn mark_idle(idle_since: &Arc<Mutex<Option<Instant>>>) {
    if let Ok(mut slot) = idle_since.lock() {
        *slot = Some(Instant::now());
    }
}

fn normalize_history(history: &[RunnerMessage]) -> Result<Vec<Item>, RunnerError> {
    let mut items = Vec::with_capacity(history.len());
    for message in history {
        match message.role.as_str() {
            "user" => {
                items.push(Item::text(ItemKind::User, &message.content));
            }
            "assistant" => {
                let mut parts: Vec<Part> = Vec::new();
                if !message.content.is_empty() {
                    parts.push(Part::Text(TextPart::new(message.content.clone())));
                }
                for call in &message.tool_calls {
                    let input: Value = if call.arguments.is_empty() {
                        Value::Object(Default::default())
                    } else {
                        serde_json::from_str(&call.arguments).map_err(|source| {
                            RunnerError::ToolCallArguments {
                                id: call.id.clone(),
                                source,
                            }
                        })?
                    };
                    parts.push(Part::ToolCall(ToolCallPart::new(
                        call.id.clone(),
                        call.name.clone(),
                        input,
                    )));
                }
                items.push(Item::new(ItemKind::Assistant, parts));
            }
            "tool" => {
                let call_id = message
                    .tool_call_id
                    .as_deref()
                    .filter(|s| !s.is_empty())
                    .ok_or(RunnerError::MissingToolCallId)?;
                items.push(Item::new(
                    ItemKind::Tool,
                    vec![Part::ToolResult(ToolResultPart::success(
                        call_id,
                        ToolOutput::text(message.content.clone()),
                    ))],
                ));
            }
            "system" => {
                items.push(Item::text(ItemKind::System, &message.content));
            }
            other => {
                return Err(RunnerError::UnsupportedHistoryRole(other.to_string()));
            }
        }
    }
    Ok(items)
}
