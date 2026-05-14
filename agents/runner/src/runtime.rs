use std::panic::AssertUnwindSafe;
use std::path::PathBuf;
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
    CatalogReader, CompositePermissionChecker, PathPolicy, PermissionDecision, ToolRegistry,
};
use dashmap::DashMap;
use dashmap::DashSet;
use futures::FutureExt;
use serde_json::Value;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};
use tokio::sync::{OnceCell, oneshot};

use agentkit_compaction::AgentBuilderCompactorExt;

use crate::clip::ClippedToolSource;
use crate::compaction::build_compactor;
use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::{McpRotatingClient, TokenRegistry, build_http};
use crate::tools;
use crate::wire::{McpServer, RunnerMessage, ThreadBootstrap};
use crate::workdir::ASSISTANT_WORKDIR;

const TOOL_RESULT_SPILL_DIR: &str = "tool-results";
const MCP_CMD_CAPACITY: usize = 32;

/// How long a thread's per-task state can sit idle before the host evicts
/// it. The VM stays alive across all per-thread events; only individual
/// thread tasks expire.
pub const DEFAULT_THREAD_IDLE_TTL: Duration = Duration::from_secs(30 * 60);

/// How often the eviction sweep runs. Picked to keep the worst-case
/// over-retention small relative to the TTL while still being cheap.
const EVICTION_SWEEP_INTERVAL: Duration = Duration::from_secs(60);

pub type AppState = Arc<RuntimeHost>;

/// Singleton host shared by every per-thread task on the VM. Owns the
/// shared bearer registry, the MCP actor handle (one connection per
/// server, shared across threads), and the bootstrap client.
pub struct RuntimeHost {
    pub assistant_id: String,
    pub started_at: Instant,
    pub tokens: TokenRegistry,
    pub seen: DashSet<String>,
    pub threads: DashMap<String, Arc<OnceCell<Arc<ConfiguredThread>>>>,
    pub gram_client: GramBootstrapClient,
    pub thread_idle_ttl: Duration,
    pub http_client: reqwest::Client,
    /// MCP server set the host has registered with its actor. Reconciled
    /// lazily when a thread's bootstrap brings new servers; existing
    /// connections are preserved across reconciliation. `mcp_cmd_tx`
    /// addresses the actor; `mcp_source` is the read-side handle every
    /// per-thread agent uses to discover tools.
    pub mcp: tokio::sync::Mutex<McpHostState>,
    pub mcp_cmd_tx: mpsc::Sender<McpCmd>,
    pub mcp_catalog: CatalogReader,
    pub spill_root: PathBuf,
}

pub struct McpHostState {
    pub registered: Vec<McpServer>,
    /// Held to keep the actor task alive for the lifetime of the host.
    /// Drop on host teardown ends the actor; we never abort it explicitly.
    #[allow(dead_code)]
    pub actor: tokio::task::JoinHandle<()>,
}

/// Live per-thread state. Every active thread on the VM has exactly one
/// `ConfiguredThread`; concurrent first-turn requests for the same thread
/// race through an `OnceCell` so only one bootstrap fetch and one task
/// spawn happen.
pub struct ConfiguredThread {
    pub thread_id: String,
    pub chat_id: String,
    pub idle_since: Arc<Mutex<Option<Instant>>>,
    pub inbox_tx: UnboundedSender<String>,
    pub task_handle: Mutex<Option<tokio::task::JoinHandle<()>>>,
}

pub enum McpCmd {
    /// Force a server to disconnect and reconnect from scratch.
    ForceReconnect {
        server_id: McpServerId,
        reply: oneshot::Sender<Result<(), String>>,
    },
    /// Register a server that the actor has not yet seen. Idempotent — the
    /// actor skips it if already registered.
    Register {
        config: McpServerConfig,
        reply: oneshot::Sender<Result<(), String>>,
    },
}

impl ConfiguredThread {
    pub fn idle_for(&self) -> Duration {
        let guard = match self.idle_since.lock() {
            Ok(g) => g,
            Err(_) => return Duration::ZERO,
        };
        match *guard {
            None => Duration::ZERO,
            Some(t) => Instant::now().saturating_duration_since(t),
        }
    }

    pub fn enqueue(&self, input: String) -> Result<(), RunnerError> {
        self.inbox_tx
            .send(input)
            .map_err(|_| RunnerError::SubmitInput("loop inbox closed".into()))?;
        mark_busy(&self.idle_since);
        Ok(())
    }
}

pub async fn build_host(
    assistant_id: String,
    server_url: String,
    initial_token: String,
    thread_idle_ttl: Duration,
) -> Result<Arc<RuntimeHost>, RunnerError> {
    let tokens = TokenRegistry::new(initial_token);

    let mut default_headers = http::HeaderMap::new();
    default_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .build()?;

    let manager = McpServerManager::new();
    let spill_root = PathBuf::from(ASSISTANT_WORKDIR).join(TOOL_RESULT_SPILL_DIR);
    let mcp_catalog = manager.source();

    let (mcp_cmd_tx, mcp_cmd_rx) = mpsc::channel(MCP_CMD_CAPACITY);
    let mcp_actor = tokio::spawn(run_mcp_actor(manager, mcp_cmd_rx));

    let gram_client = GramBootstrapClient::new(
        server_url.clone(),
        http_client.clone(),
        tokens.clone(),
    );

    let _ = server_url; // captured by GramBootstrapClient
    let host = Arc::new(RuntimeHost {
        assistant_id,
        started_at: Instant::now(),
        tokens,
        seen: DashSet::new(),
        threads: DashMap::new(),
        gram_client,
        thread_idle_ttl,
        http_client,
        mcp: tokio::sync::Mutex::new(McpHostState {
            registered: Vec::new(),
            actor: mcp_actor,
        }),
        mcp_cmd_tx,
        mcp_catalog,
        spill_root,
    });

    // Background eviction task: walks the threads map and drops any whose
    // idle clock has run past the TTL. Runs for the lifetime of the host.
    let evict_host = Arc::clone(&host);
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(EVICTION_SWEEP_INTERVAL);
        interval.tick().await;
        loop {
            interval.tick().await;
            sweep_idle(&evict_host).await;
        }
    });

    Ok(host)
}

/// Snapshot active threads — used by /state and the eviction sweep.
pub fn snapshot_threads(host: &RuntimeHost) -> Vec<(String, String, Duration)> {
    host.threads
        .iter()
        .filter_map(|entry| {
            let cell = entry.value().clone();
            cell.get().map(|thread| {
                (
                    thread.thread_id.clone(),
                    thread.chat_id.clone(),
                    thread.idle_for(),
                )
            })
        })
        .collect()
}

async fn sweep_idle(host: &Arc<RuntimeHost>) {
    let ttl = host.thread_idle_ttl;
    let mut to_evict = Vec::new();
    for entry in host.threads.iter() {
        let cell = entry.value().clone();
        if let Some(thread) = cell.get()
            && thread.idle_for() > ttl
        {
            to_evict.push(thread.thread_id.clone());
        }
    }
    for thread_id in to_evict {
        if let Some((_, cell)) = host.threads.remove(&thread_id)
            && let Some(thread) = cell.get()
        {
            tracing::info!(thread_id = %thread_id, "evicting idle thread");
            // Closing the inbox causes run_loop to return; abort the task
            // for prompt teardown of any blocked compactor / model call.
            if let Ok(mut handle_slot) = thread.task_handle.lock()
                && let Some(handle) = handle_slot.take()
            {
                handle.abort();
            }
        }
    }
}

/// First-turn bootstrap path. Concurrent /turn requests for the same thread
/// race through the `OnceCell`; only one wins the bootstrap fetch and task
/// spawn. Subsequent turns (cached cell) skip directly to enqueue.
pub async fn ensure_thread(
    host: &Arc<RuntimeHost>,
    thread_id: &str,
) -> Result<Arc<ConfiguredThread>, RunnerError> {
    let cell = host
        .threads
        .entry(thread_id.to_string())
        .or_insert_with(|| Arc::new(OnceCell::new()))
        .clone();

    let thread = cell
        .get_or_try_init(|| async {
            let bootstrap = host
                .gram_client
                .fetch_bootstrap(thread_id)
                .await
                .map_err(|e| RunnerError::Loop(format!("bootstrap fetch failed: {e}")))?;
            spawn_thread(host, thread_id.to_string(), bootstrap).await
        })
        .await?;
    Ok(thread.clone())
}

/// Builds a per-thread agent and spawns its tokio task. Each task is wrapped
/// in `catch_unwind` so a panic inside one thread's tool call, stream
/// parser, or MCP client does not take down the VM or sibling threads.
async fn spawn_thread(
    host: &Arc<RuntimeHost>,
    thread_id: String,
    bootstrap: ThreadBootstrap,
) -> Result<Arc<ConfiguredThread>, RunnerError> {
    // Reconcile MCP set additively. Removed servers stay registered; the
    // model can still call them but they go unused. A future pass can
    // disconnect them; for now, last-writer-wins additive avoids a
    // destructive race when two threads bootstrap with different sets.
    register_missing_servers(host, &bootstrap.mcp_servers).await?;

    let chat_id = bootstrap.chat_id.clone();

    // Per-thread completions adapter. Outbound /chat/completions calls
    // carry the thread's chat id so the server's revalidation check can
    // confirm the chat belongs to the assistant on the JWT.
    let mut chat_headers = http::HeaderMap::new();
    chat_headers.insert(
        http::HeaderName::from_static("gram-chat-id"),
        http::HeaderValue::from_str(&chat_id)
            .map_err(|source| RunnerError::HeaderValue { source })?,
    );
    chat_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let thread_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(chat_headers)
        .build()?;

    let openrouter_config =
        OpenRouterConfig::new(String::new(), bootstrap.model.clone())
            .with_base_url(bootstrap.completions_url.clone());
    let provider = OpenRouterProvider::from(openrouter_config);

    let completions_http = build_http(thread_http_client.clone(), host.tokens.clone());
    let adapter = CompletionsAdapter::with_client(provider.clone(), completions_http);

    // Compactor outbound headers omit Gram-Chat-ID so the server's chat
    // capture pipeline does not mistake the compactor's "summarise this
    // transcript" turn for divergence on the user's chat.
    let mut compactor_headers = http::HeaderMap::new();
    compactor_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let compactor_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(compactor_headers)
        .build()?;
    let compactor_http = build_http(compactor_http_client, host.tokens.clone());
    let compactor_adapter = CompletionsAdapter::with_client(provider, compactor_http);

    let compactor = build_compactor(
        &bootstrap.chat_id,
        bootstrap.context_window.unwrap_or(0),
        compactor_adapter,
    )?;

    let mut transcript = Vec::new();
    if !bootstrap.instructions.is_empty() {
        transcript.push(Item::text(ItemKind::System, &bootstrap.instructions));
    }
    transcript.extend(normalize_history(&bootstrap.history)?);

    let permissions = CompositePermissionChecker::new(PermissionDecision::Allow).with_policy(
        PathPolicy::new()
            .allow_root(ASSISTANT_WORKDIR)
            .read_only_root(format!("{ASSISTANT_WORKDIR}/node_modules"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/browser.ts"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/package.json")),
    );
    let fs_resources = FileSystemToolResources::new()
        .with_policy(FileSystemToolPolicy::new().require_read_before_write(true));

    let mcp_server_ids: Vec<String> = bootstrap
        .mcp_servers
        .iter()
        .map(|s| s.id.clone())
        .collect();
    let native_tools = ToolRegistry::new().with(tools::bun_run::bun_run).with(
        tools::mcp_force_reconnect::McpForceReconnectTool::new(
            host.mcp_cmd_tx.clone(),
            mcp_server_ids,
        ),
    );

    let mcp_source = ClippedToolSource::new(host.mcp_catalog.clone(), host.spill_root.clone());
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

    let session = SessionConfig::new(bootstrap.chat_id.clone())
        .with_cache(PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short));
    let driver = agent
        .start(session)
        .await
        .map_err(|e| RunnerError::AgentStart(e.to_string()))?;

    let idle_since = Arc::new(Mutex::new(Some(Instant::now())));
    let (inbox_tx, inbox_rx) = mpsc::unbounded_channel::<String>();
    let loop_idle = Arc::clone(&idle_since);
    let log_thread_id = thread_id.clone();
    let host_for_eviction = Arc::clone(host);
    let evict_thread_id = thread_id.clone();

    let task_handle = tokio::spawn(async move {
        let outcome = AssertUnwindSafe(run_loop(driver, inbox_rx, loop_idle))
            .catch_unwind()
            .await;
        match outcome {
            Ok(Ok(reason)) => tracing::info!(thread_id = %log_thread_id, reason = %reason, "thread loop exited"),
            Ok(Err(err)) => tracing::error!(thread_id = %log_thread_id, error = %err, "thread loop exited with error"),
            Err(panic_payload) => {
                let msg = panic_payload
                    .downcast_ref::<&'static str>()
                    .map(|s| (*s).to_string())
                    .or_else(|| {
                        panic_payload
                            .downcast_ref::<String>()
                            .cloned()
                    })
                    .unwrap_or_else(|| "<panic payload>".to_string());
                tracing::error!(thread_id = %log_thread_id, panic = %msg, "thread loop panicked");
            }
        }
        // Drop the entry on exit so a stale ConfiguredThread doesn't keep
        // holding state for a dead task.
        host_for_eviction.threads.remove(&evict_thread_id);
    });

    let configured = Arc::new(ConfiguredThread {
        thread_id,
        chat_id,
        idle_since,
        inbox_tx,
        task_handle: Mutex::new(Some(task_handle)),
    });
    Ok(configured)
}

async fn register_missing_servers(
    host: &Arc<RuntimeHost>,
    incoming: &[McpServer],
) -> Result<(), RunnerError> {
    if incoming.is_empty() {
        return Ok(());
    }
    let mut state = host.mcp.lock().await;
    let known: std::collections::HashSet<&str> =
        state.registered.iter().map(|s| s.id.as_str()).collect();
    let new_servers: Vec<McpServer> = incoming
        .iter()
        .filter(|s| !known.contains(s.id.as_str()))
        .cloned()
        .collect();
    drop(known);
    for server in &new_servers {
        let config = build_mcp_server_config(server, &host.http_client, &host.tokens)?;
        let (reply_tx, reply_rx) = oneshot::channel();
        if host
            .mcp_cmd_tx
            .send(McpCmd::Register {
                config,
                reply: reply_tx,
            })
            .await
            .is_err()
        {
            return Err(RunnerError::Loop("mcp actor channel closed".into()));
        }
        match reply_rx.await {
            Ok(Ok(())) => state.registered.push(server.clone()),
            Ok(Err(e)) => tracing::warn!(server_id = %server.id, error = %e, "register mcp server failed"),
            Err(_) => return Err(RunnerError::Loop("mcp actor reply dropped".into())),
        }
    }
    Ok(())
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

async fn run_mcp_actor(
    mut manager: McpServerManager,
    mut cmd_rx: mpsc::Receiver<McpCmd>,
) {
    while let Some(cmd) = cmd_rx.recv().await {
        match cmd {
            McpCmd::Register { config, reply } => {
                let server_id = config.id.clone();
                manager.register_server(config);
                let result = match manager.connect_server(&server_id).await {
                    Ok(handle) => {
                        tracing::info!(
                            server_id = %server_id,
                            tools = handle.snapshot().tools.len(),
                            "mcp register ok"
                        );
                        Ok(())
                    }
                    Err(e) => {
                        tracing::warn!(server_id = %server_id, error = %e, "mcp register/connect failed");
                        Err(e.to_string())
                    }
                };
                let _ = reply.send(result);
            }
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

async fn run_loop<S>(
    mut driver: LoopDriver<S>,
    mut inbox: UnboundedReceiver<String>,
    idle_since: Arc<Mutex<Option<Instant>>>,
) -> Result<&'static str, RunnerError>
where
    S: ModelSession,
{
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
